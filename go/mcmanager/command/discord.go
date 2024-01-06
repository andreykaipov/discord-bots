package command

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/bwmarrin/discordgo"
	"github.com/google/go-github/v57/github"
	"gopkg.in/yaml.v3"
)

type Discord struct {
	Command

	DiscordToken      string `required:"" env:"DISCORD_TOKEN"`
	ManagementChannel string `required:"" env:"MGMT_CHANNEL" name:"mgmt-channel" help:"A channel ID to listen for management commands in"`
	discord           *discordgo.Session

	AzureTenantID       string `required:"" env:"AZURE_TENANT_ID" help:"The Azure Tenant ID"`
	AzureClientID       string `required:"" env:"AZURE_CLIENT_ID" help:"The Azure Client ID"`
	AzureClientSecret   string `required:"" env:"AZURE_CLIENT_SECRET" help:"The Azure Client Secret"`
	AzureSubscriptionID string `required:"" env:"AZURE_SUBSCRIPTION_ID" help:"The Azure Subscription ID"`
	vmClient            *armcompute.VirtualMachinesClient

	ServersFile  *os.File `required:"" env:"SERVERS_FILE" help:"A path to a file containing the servers to monitor"`
	serverConfig *serverConfig

	// unused
	AppID          int64  `hidden:"" env:"GH_APP_ID" help:"The GitHub App ID"`
	InstallationID int64  `hidden:"" env:"GH_INSTALLATION_ID" help:"The GitHub App Installation ID"`
	AppKeyFile     string `hidden:"" type:"path" env:"GH_APP_KEY_FILE" help:"A path to a file containing the private key for the GitHub App"`
}

func (c *Discord) setupDiscord() {
	dg, err := discordgo.New("Bot " + c.DiscordToken)
	c.Kong.FatalIfErrorf(err, "failed creating Discord session")

	dg.AddHandler(c.onMessageCreate)
	dg.Identify.Intents |= discordgo.IntentsAllWithoutPrivileged
	dg.Identify.Intents |= discordgo.IntentsMessageContent
	err = dg.Open()
	c.Kong.FatalIfErrorf(err, "failed opening connection to Discord")
	c.discord = dg
}

func (c *Discord) setupAzure() {
	creds, err := azidentity.NewDefaultAzureCredential(nil)
	c.Kong.FatalIfErrorf(err, "failed getting credentials")

	c.vmClient, err = armcompute.NewVirtualMachinesClient(c.AzureSubscriptionID, creds, nil)
	c.Kong.FatalIfErrorf(err, "failed creating vm client")
}

func (c *Discord) AfterApply() error {
	c.setupDiscord()
	c.setupAzure()
	return nil
}

func (c *Discord) Run() error {
	rawServers, err := io.ReadAll(c.ServersFile)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(rawServers, &c.serverConfig); err != nil {
		return err
	}
	if err := c.setConfigDefaults(); err != nil {
		return err
	}

	wg := sync.WaitGroup{}
	for _, s := range c.serverConfig.Servers {
		wg.Add(1)
		go func(s *server) {
			defer wg.Done()
			ticker := time.NewTicker(s.CheckInterval)
			defer ticker.Stop()
			for ; true; <-ticker.C {
				c.deallocateCondionally(s)
			}
		}(s)

		// periodically check if the server is online because it might
		// have been started outside of the typical .start/.stop
		// discord commands (e.g. via a Terraform apply, az start, from
		// the UI). it's mostly a safeguard.
		wg.Add(1)
		go func(s *server) {
			defer wg.Done()
			ticker := time.NewTicker(1 * time.Hour)
			defer ticker.Stop()
			for ; true; <-ticker.C {
				c.Kong.Printf("periodic check of server's online status: %s", s.Host)
				s.online = false
				if _, err := c.checkServer(s); err == nil {
					s.online = true
				}
			}
		}(s)
	}
	wg.Wait()
	return nil
}

// will only deallocate if the server has errored or has zero players for
// consecutive checks equal to the deallocation threshold
func (c *Discord) deallocateCondionally(s *server) {
	if !s.online {
		return
	}

	c.Kong.Printf("checking server: %s", s.Host)
	pong, err := c.checkServer(s)
	if err != nil {
		c.Kong.Printf("error checking server: %s: %s", s.Host, err)
		s.checkErrors++
	} else {
		s.online = true
		switch pong.PlayerCount {
		case 0:
			s.checkCount++
			msg := fmt.Sprintf("%s has no players online (check count: %d)", s.Host, s.checkCount)
			c.Kong.Printf(msg)
			// _ = c.sendMessagef(msg)
		default:
			s.checkCount = 0
			s.checkErrors = 0
		}
	}

	var msg string
	if s.checkErrors >= s.DeallocationThreshold {
		msg = fmt.Sprintf("%s deallocating because it had %d consecutive errors", s.Host, s.DeallocationThreshold)
		s.online = false
	}
	if s.checkCount >= s.DeallocationThreshold {
		total := time.Duration(s.DeallocationThreshold) * s.CheckInterval
		msg = fmt.Sprintf("%s deallocating because it had no players for %s", s.Host, total)
		s.online = false
	}

	// we've mark errored servers or servers with no online
	// players as offline before deallocation so we don't
	// try to check them again
	if !s.online {
		c.Kong.Printf(msg)
		_ = c.sendMessagef(msg)
		go c.deallocateServer(s)
	}

	// c.Kong.Printf(pong.Pretty())
}

func (c *Discord) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.ChannelID != c.ManagementChannel {
		return
	}
	if m.Author.ID == s.State.User.ID {
		return
	}
	if !strings.HasPrefix(m.Content, ".") {
		return
	}

	cmdArgs := strings.SplitN(m.Content, " ", 2)
	cmd := cmdArgs[0]
	args := ""
	if len(cmdArgs) > 1 {
		args = strings.TrimSpace(cmdArgs[1])
	}

	var msg string
	switch cmd {
	case ".help":
		msg = `
.help - show this help message
.ping - pong
.uptime - show uptime of this bot
.list - list servers
.info <server> - show server info
.start <server> - start a server
.stop <server> - stop a server
`
	case ".ping":
		s.ChannelMessageSend(m.ChannelID, "pong")
	case ".uptime":
		host, _ := os.Hostname()
		uptime := time.Since(c.startTime)
		msg = fmt.Sprintf(`
host: %s
uptime: %s
`, host, uptime)
	case ".list":
		var servers []string
		for _, server := range c.serverConfig.Servers {
			status := "offline"
			if server.online {
				status = "online"
			}
			servers = append(servers, fmt.Sprintf("%-10s%s:%s", "["+status+"]", server.host, server.port))
		}
		msg = strings.Join(servers, "\n")
	case ".info":
		c.discord.ChannelTyping(m.ChannelID)
		val := args
		if val == "" {
			msg = "usage: .info <server>"
			break
		}
		s, err := c.findServerFuzzy(val)
		if err != nil {
			msg = fmt.Sprintf("error finding server:\n%s", err)
			break
		}
		if err := c.setServerDefaults(s); err != nil {
			msg = fmt.Sprintf("error setting defaults:\n%s", err)
			break
		}
		_ = c.sendMessagef("%s", s.Name)
		pong, err := c.checkServer(s)
		if err != nil {
			msg = fmt.Sprintf("error checking %s:\n%s", s.Name, err)
			break
		}
		msg = pong.Pretty()
	case ".start":
		c.discord.ChannelTyping(m.ChannelID)
		val := args
		if val == "" {
			msg = "usage: .start <server>"
			break
		}
		s, err := c.findServerFuzzy(val)
		if err != nil {
			msg = fmt.Sprintf("error finding server:\n%s", err)
			break
		}
		if err := c.setServerDefaults(s); err != nil {
			msg = fmt.Sprintf("error setting defaults:\n%s", err)
			break
		}
		_ = c.sendMessagef("received start request for %s", s.Name)
		msg, err = c.startServer(s)
		if err != nil {
			msg = fmt.Sprintf("error starting %s:\n%s", s.Name, err)
			break
		}
	case ".stop":
		c.discord.ChannelTyping(m.ChannelID)
		val := args
		if val == "" {
			msg = "usage: .stop <server>"
			break
		}
		s, err := c.findServerFuzzy(val)
		if err != nil {
			msg = fmt.Sprintf("error finding server:\n%s", err)
			break
		}
		if err := c.setServerDefaults(s); err != nil {
			msg = fmt.Sprintf("error setting defaults:\n%s", err)
			break
		}
		_ = c.sendMessagef("received deallocation request for %s", s.Name)
		msg, err = c.deallocateServer(s)
		if err != nil {
			msg = fmt.Sprintf("error deallocating %s:\n%s", s.Name, err)
			break
		}
	default:
		msg = "unknown command, try .help"
	}

	if err := c.sendMessagef(msg); err != nil {
		c.Kong.Errorf("sending message: %v", err)
	}
}

func (c *Discord) sendMessagef(format string, a ...any) error {
	msg := strings.TrimSpace(format)
	msg = fmt.Sprintf(msg, a...)
	if msg == "" {
		msg = "ok"
	}
	msg = fmt.Sprintf("```\n%s\n```", msg)
	if len(msg) > 1000 {
		r := strings.NewReader(strings.TrimSpace(msg))
		_, err := c.discord.ChannelFileSend(c.ManagementChannel, "output.txt", r)
		if err != nil {
			return err
		}
	} else {
		_, err := c.discord.ChannelMessageSend(c.ManagementChannel, msg)
		if err != nil {
			return err
		}
	}
	return nil
}

// dispatches a workflow
// wrote this and then realized i'll just use az deallocate ...
func (c *Discord) Dispatch() error {
	tr, err := ghinstallation.NewKeyFromFile(http.DefaultTransport, c.AppID, c.InstallationID, c.AppKeyFile)
	if err != nil {
		return err
	}

	client := github.NewClient(&http.Client{Transport: tr})
	resp, err := client.Actions.CreateWorkflowDispatchEventByFileName(
		context.Background(),
		"andreykaipov",
		"self",
		"infra.repo.dispatch.yml",
		github.CreateWorkflowDispatchEventRequest{
			Ref: "main",
			Inputs: map[string]interface{}{
				"message": "Hello, World!",
			},
		},
	)
	if err != nil {
		return err
	}
	fmt.Println(resp)
	return nil
}
