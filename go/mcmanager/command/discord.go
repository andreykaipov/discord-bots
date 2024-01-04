package command

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
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
	if err := c.serverConfig.setDefaults(); err != nil {
		return err
	}

	cfg := c.serverConfig
	ticker := time.NewTicker(cfg.CheckInterval)
	for ; true; <-ticker.C {
		for _, server := range cfg.Servers {
			if !server.online {
				continue
			}
			c.Kong.Printf("checking server: %s", server.Host)
			pong, err := c.checkServer(server)
			if err != nil {
				// should this count towards the check count?
				c.Kong.Printf("error checking server: %s: %s", server.Host, err)
				continue
			}

			switch pong.PlayerCount {
			case 0:
				server.checkCount++
				msg := fmt.Sprintf("%s has no players online (check count: %d)", server.Host, server.checkCount)
				c.Kong.Printf(msg)
				// _ = c.sendMessagef(msg)
			default:
				server.checkCount = 0
			}

			if server.checkCount >= cfg.DeallocationThreshold {
				total := time.Duration(cfg.DeallocationThreshold) * cfg.CheckInterval
				msg := fmt.Sprintf("%s deallocating because it had no players for %s", server.Host, total)
				c.Kong.Printf(msg)
				_ = c.sendMessagef(msg)
				go c.deallocateServer(server)
				server.online = false // mark it offline earlier so we don't try to check it again
			}

			// c.Kong.Printf(pong.Pretty())
		}
	}
	return nil
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

	var msg string
	var err error
	switch {
	case m.Content == ".help":
		msg = `
.help - show this help message
.ping - pong
.list - list servers
.info <server> - show server info
.start <server> - start a server
.stop <server> - stop a server
`
	case m.Content == ".ping":
		s.ChannelMessageSend(m.ChannelID, "pong")
	case m.Content == ".list":
		var servers []string
		for _, server := range c.serverConfig.Servers {
			servers = append(servers, fmt.Sprintf("%s:%s", server.host, server.port))
		}
		msg = strings.Join(servers, "\n")
	case strings.HasPrefix(m.Content, ".info "):
		c.discord.ChannelTyping(m.ChannelID)
		val := strings.TrimSpace(strings.TrimPrefix(m.Content, ".info"))
		if val == "" {
			msg = "usage: .info <server>"
			break
		}
		s := c.findServerFuzzy(val)
		if err := s.setDefaults(); err != nil {
			msg = fmt.Sprintf("error setting defaults:\n%s", err)
			break
		}
		ping := &Ping{}
		pong, err := ping.Check(s.host, s.port, s.Timeout)
		if err != nil {
			msg = fmt.Sprintf("error checking %s:\n%s", s.Name, err)
			break
		}
		msg = fmt.Sprintf("%s\n%s", s.Name, pong.Pretty())
	case strings.HasPrefix(m.Content, ".start "):
		c.discord.ChannelTyping(m.ChannelID)
		val := strings.TrimSpace(strings.TrimPrefix(m.Content, ".start"))
		if val == "" {
			msg = "usage: .start <server>"
			break
		}
		s := c.findServerFuzzy(val)
		if err := s.setDefaults(); err != nil {
			msg = fmt.Sprintf("error setting defaults:\n%s", err)
			break
		}
		_ = c.sendMessagef("received start request for %s", s.Name)
		msg, err = c.startServer(s)
		if err != nil {
			msg = fmt.Sprintf("error starting %s:\n%s", s.Name, err)
			break
		}
	case strings.HasPrefix(m.Content, ".stop "):
		c.discord.ChannelTyping(m.ChannelID)
		val := strings.TrimSpace(strings.TrimPrefix(m.Content, ".stop"))
		if val == "" {
			msg = "usage: .stop <server>"
			break
		}
		s := c.findServerFuzzy(val)
		if err := s.setDefaults(); err != nil {
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
	msg = fmt.Sprintf("```%s```", msg)
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
