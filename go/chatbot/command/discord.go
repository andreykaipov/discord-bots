package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/andreykaipov/discord-bots/go/chatbot/pkg"
	"github.com/bwmarrin/discordgo"
	openai "github.com/sashabaranov/go-openai"
	"gopkg.in/yaml.v3"
)

type Discord struct {
	Command

	DiscordToken      string `required:"" env:"DISCORD_TOKEN"`
	ChatChannel       string `required:"" env:"CHAT_CHANNEL" help:"A channel ID to chat with the bot"`
	ManagementChannel string `required:"" env:"MGMT_CHANNEL" name:"mgmt-channel" help:"A channel ID to listen for management commands in"`
	discord           *discordgo.Session

	OpenAIAPIKey string   `name:"openai-api-key" required:"" env:"OPENAI_API_KEY"`
	Model        string   `optional:"" name:"model" env:"MODEL"`
	Temperature  float32  `optional:"" default:"1" env:"TEMPERATURE"`
	TopP         float32  `optional:"" default:"1" env:"TOP_P"`
	Prompts      *os.File `required:"" name:"prompts" env:"PROMPTS"`
	prompts      *prompts
	rawPrompts   []byte
	personality  string   // the current prompt name
	Users        *os.File `required:"" name:"users" env:"USERS"`
	users        map[string]string
	rawUsers     []byte
	openai       *openai.Client

	MessageContext             int          `optional:"" default:"20" env:"MESSAGE_CONTEXT" help:"The number of previous messages to send back to OpenAI"`
	MessageContextInterval     int          `optional:"" default:"90" env:"MESSAGE_CONTEXT_INTERVAL" help:"The time in seconds until previous message context is reset, if no new messages are received"`
	MessageReplyInterval       int          `optional:"" default:"1" env:"MESSAGE_REPLY_INTERVAL" help:"The base time in seconds after a message is received to wait before sending a reply"`
	MessageReplyIntervalJitter int          `optional:"" default:"4" env:"MESSAGE_REPLY_INTERVAL_JITTER" help:"A randomized time [0,n) in seconds to add to the base message reply interval"`
	MessageSelfReplyChance     int          `optional:"" default:"10" env:"MESSAGE_SELF_REPLY_CHANCE" help:"The percent chance that the bot will reply twice in a row"`
	messageReplyTicker         *time.Ticker // interval for determining message reply
	messageContextTicker       *time.Ticker // interval for resetting message context
	messages                   *pkg.LimitedQueue[openai.ChatCompletionMessage]
	replying                   bool
}

type prompts struct {
	Meta struct {
		Prefix string `yaml:"prefix"`
		Suffix string `yaml:"suffix"`
	} `yaml:"meta"`
	Personalities map[string]string `yaml:"personalities"`
}

func (c *Discord) AfterApply() error {
	if c.ChatChannel == c.ManagementChannel {
		return errors.New("chat and management channels cannot be the same")
	}

	dg, err := discordgo.New("Bot " + c.DiscordToken)
	if err != nil {
		return fmt.Errorf("error creating Discord session: %w", err)
	}
	dg.AddHandler(c.onMessageCreate)
	dg.Identify.Intents |= discordgo.IntentsAllWithoutPrivileged
	dg.Identify.Intents |= discordgo.IntentsMessageContent
	if err := dg.Open(); err != nil {
		return fmt.Errorf("error opening connection: %w", err)
	}
	c.discord = dg
	c.messageReplyTicker = time.NewTicker(1 * time.Second)
	c.messageContextTicker = time.NewTicker(1 * time.Second)

	if c.openai == nil {
		c.openai = openai.NewClient(c.OpenAIAPIKey)
	}
	if c.Model == "" {
		c.Model = openai.GPT3Dot5Turbo
	}

	if c.messages == nil {
		c.messages = pkg.NewLimitedQueue[openai.ChatCompletionMessage](c.MessageContext)
	}
	if err := c.parsePrompts(); err != nil {
		return err
	}
	if err := c.parseUsers(); err != nil {
		return err
	}

	return nil
}

func (c *Discord) parsePrompts() error {
	var err error
	if c.rawPrompts, err = io.ReadAll(c.Prompts); err != nil {
		return err
	}
	if err := yaml.Unmarshal(c.rawPrompts, &c.prompts); err != nil {
		return err
	}

	personalities := c.prompts.Personalities
	for name, prompt := range personalities {
		personalities[name] = fmt.Sprintf("%s%s%s", c.prompts.Meta.Prefix, prompt, c.prompts.Meta.Suffix)
	}

	return nil
}

func (c *Discord) parseUsers() error {
	var err error
	if c.rawUsers, err = io.ReadAll(c.Users); err != nil {
		return err
	}
	if err := json.Unmarshal(c.rawUsers, &c.users); err != nil {
		return err
	}
	return nil
}

func (c *Discord) Run() error {
	fmt.Println("Bot is now running. Press CTRL-C to exit.")

	defer func() {
		_ = c.discord.Close()
		//if _, err := c.discord.ChannelMessageSend(c.ManagementChannel, "shutting down..."); err != nil {
		//	fmt.Printf("error sending message: %v\n", err)
		//}
	}()
	//if _, err := c.discord.ChannelMessageSend(c.ManagementChannel, "started up!"); err != nil {
	//	fmt.Printf("error sending message: %v\n", err)
	//}

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	c.resetMessageQueue("")
	c.resetMessageTickers()
	defer c.messageReplyTicker.Stop()
	defer c.messageContextTicker.Stop()

	for {
		select {
		case <-c.messageReplyTicker.C:
			c.attemptSendReply(c.ChatChannel)
		case <-c.messageContextTicker.C:
			// if the context triggers while we're replying, or if
			// the queue is already empty, don't reset the queue
			if c.replying || len(c.messages.Items()) == 0 {
				continue
			}
			// choose a random personality on reset
			c.resetMessageQueue("")
		case <-sc:
			return nil
		}
	}
}

func (c *Discord) resetMessageQueue(personality string) {
	defer func() {
		c.Kong.Printf("reset limited queue, current prompt: %s", c.personality)
	}()

	c.personality = personality
	if personality == "" {
		c.personality, _ = getRandom(c.prompts.Personalities)
	}

	prompt := c.prompts.Personalities[c.personality]

	c.messages = pkg.NewLimitedQueue[openai.ChatCompletionMessage](c.MessageContext)
	c.messages.AddSticky(openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: prompt,
	})
}

func (c *Discord) resetMessageTickers() {
	c.messageReplyTicker.Reset(time.Duration(c.MessageReplyInterval+rand.Intn(c.MessageReplyIntervalJitter)) * time.Second)
	c.messageContextTicker.Reset(time.Duration(c.MessageContextInterval) * time.Second)
	c.replying = true
}

func (c *Discord) attemptSendReply(channel string) {
	fmt.Printf("attempting to send reply\n")

	// don't reply if there are no messages
	if len(c.messages.Items()) == 0 {
		c.messageReplyTicker.Stop()
		c.replying = false
		return
	}

	// chance to reply to ourselves, unless we already have (i.e. last two
	// messages were from the assistant)
	if c.messages.LastN(1).Role == openai.ChatMessageRoleAssistant {
		if c.messages.LastN(2).Role == openai.ChatMessageRoleAssistant || rand.Intn(100) < 100-c.MessageSelfReplyChance {
			c.messageReplyTicker.Stop()
			c.replying = false
			return
		}
	}

	_ = c.discord.ChannelTyping(channel)
	reply := c.makeChatRequestWithMessages(c.messages.AllItems())

	if _, err := c.discord.ChannelMessageSend(channel, reply); err != nil {
		fmt.Printf("error sending message: %v\n", err)
	}
}

func (c *Discord) handleManagementMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	var msg string
	switch {
	case m.Content == ".help":
		msg = `
.help - show this help message
.ping - pong
.info - show the internal settings of the bot
.reset - reset the bot
.users - show the known users
.prompts - show the available prompts
.prompts add - add a new prompt (do not include the prefix or suffix)
.set [key] [value] - set a key/value pair in the bot's settings
`
	case m.Content == ".ping":
		msg = "pong"
	case m.Content == ".reset":
		c.resetMessageQueue("")
		c.resetMessageTickers()
	case m.Content == ".users":
		msg = string(c.rawUsers)
	case m.Content == ".prompts":
		b, _ := yaml.Marshal(c.prompts.Meta)
		for name, prompt := range c.prompts.Personalities {
			prompt = strings.TrimPrefix(prompt, c.prompts.Meta.Prefix)
			prompt = strings.TrimSuffix(prompt, c.prompts.Meta.Suffix)
			b = append(b, []byte(name+": |\n")...)
			b = append(b, []byte("    "+prompt)...)
		}
		fmt.Printf("%#v\n", c.prompts.Personalities)
		msg = string(b)
	case strings.HasPrefix(m.Content, ".prompts add"):
		val := strings.TrimSpace(strings.TrimPrefix(m.Content, ".prompts add"))
		splat := strings.SplitN(val, " ", 2)
		if len(splat) != 2 {
			msg = "please provide a prompt name and prompt to add"
			break
		}
		name := splat[0]
		prompt := splat[1]
		if prompt == "" {
			delete(c.prompts.Personalities, name)
			msg = fmt.Sprintf("removed prompt %s", name)
		} else {
			c.prompts.Personalities[name] = prompt
			msg = fmt.Sprintf("added prompt %s", name)
		}
	case m.Content == ".info":
		host, _ := os.Hostname()
		uptime := time.Since(c.startTime)
		msg = fmt.Sprintf(`
host: %s
uptime: %s
model: %s
prompt: %s
top_p: %f
temperature: %f
queued_messages: %d
message_context: %d
message_context_interval: %ds
message_reply_interval: %ds
message_reply_interval_jitter: %ds
message_self_reply_chance: %d%%
`, host, uptime, c.Model, c.personality, c.Temperature, c.TopP, len(c.messages.AllItems()), c.MessageContext, c.MessageContextInterval, c.MessageReplyInterval, c.MessageReplyIntervalJitter, c.MessageSelfReplyChance)
	case strings.HasPrefix(m.Content, ".set"):
		content := strings.TrimSpace(strings.TrimPrefix(m.Content, ".set"))
		parts := strings.SplitN(content, " ", 2)
		if len(parts) != 2 {
			msg = "invalid number of arguments"
			break
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		msg = c.setKeyVal(key, val)
	default:
		msg = "unknown command, please try .help"
	}

	var err error
	if msg == "" {
		msg = "ok"
	}

	if len(msg) > 1000 {
		r := strings.NewReader(strings.TrimSpace(msg))
		_, err = c.discord.ChannelFileSend(m.ChannelID, "output.txt", r)
	} else {
		_, err = c.discord.ChannelMessageSend(m.ChannelID, "```"+strings.TrimSpace(msg)+"```")
	}
	if err != nil {
		c.Kong.Printf("error sending message: %v", err)
	}
}

// thank you copilot
func (c *Discord) setKeyVal(key, val string) string {
	switch key {
	case "model":
		c.Model = val
		return fmt.Sprintf("set model to %s", c.Model)
	case "prompt":
		if _, ok := c.prompts.Personalities[val]; !ok {
			return "please provide a valid prompt name"
		}
		c.resetMessageQueue(val)
		c.resetMessageTickers()
		return fmt.Sprintf("set prompt to %s", val)
	case "top_p":
		f, err := strconv.ParseFloat(val, 32)
		if err != nil {
			return fmt.Sprintf("error parsing top_p: %v", err)
		}
		c.TopP = float32(f)
		return fmt.Sprintf("set top_p to %f", c.TopP)
	case "temperature":
		f, err := strconv.ParseFloat(val, 32)
		if err != nil {
			return fmt.Sprintf("error parsing temperature: %v", err)
		}
		c.Temperature = float32(f)
		return fmt.Sprintf("set temperature to %f", c.Temperature)
	case "message_context":
		i, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Sprintf("error parsing message_context: %v", err)
		}
		c.MessageContext = i
		c.resetMessageQueue(c.personality)
		c.resetMessageTickers()
		return fmt.Sprintf("set message_context to %d", c.MessageContext)
	case "message_context_interval":
		i, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Sprintf("error parsing message_context_interval: %v", err)
		}
		c.MessageContextInterval = i
		c.resetMessageTickers()
		return fmt.Sprintf("set message_context_interval to %d", c.MessageContextInterval)
	case "message_reply_interval":
		i, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Sprintf("error parsing message_reply_interval: %v", err)
		}
		c.MessageReplyInterval = i
		c.resetMessageTickers()
		return fmt.Sprintf("set message_reply_interval to %d", c.MessageReplyInterval)
	case "message_reply_interval_jitter":
		i, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Sprintf("error parsing message_reply_interval_jitter: %v", err)
		}
		c.MessageReplyIntervalJitter = i
		c.resetMessageTickers()
		return fmt.Sprintf("set message_reply_interval_jitter to %d", c.MessageReplyIntervalJitter)
	case "message_self_reply_chance":
		i, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Sprintf("error parsing message_self_reply_chance: %v", err)
		}
		c.MessageSelfReplyChance = i
		return fmt.Sprintf("set message_self_reply_chance to %d", c.MessageSelfReplyChance)
	default:
		validKeys := []string{"model", "prompt", "top_p", "temperature", "message_context", "message_context_interval", "message_reply_interval", "message_reply_interval_jitter", "message_self_reply_chance"}
		return fmt.Sprintf("unknown key, valid keys are: %s", strings.Join(validKeys, ", "))
	}
}

func (c *Discord) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	switch m.ChannelID {
	case c.ManagementChannel:
		c.handleManagementMessage(s, m)
	case c.ChatChannel:
		c.handleChatMessage(s, m)
	default:
		return
	}
}

func (c *Discord) handleChatMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if strings.HasPrefix(m.Content, "//") {
		return
	}

	// replace <@id> in message with human name
	for id, name := range c.users {
		m.Content = strings.ReplaceAll(m.Content, "<@"+c.username(id)+">", name)
	}

	message := openai.ChatCompletionMessage{}

	if m.Author.ID == s.State.User.ID {
		// if it's a message from the model, don't prepend the username
		message.Role = openai.ChatMessageRoleAssistant
		message.Content = m.Content
	} else {
		// if a non-bot user sent a message, reset the ticker and start
		// typing because the bot will reply on the next ticker interval
		c.resetMessageTickers()
		c.discord.ChannelTyping(m.ChannelID)
		message.Role = openai.ChatMessageRoleUser
		message.Content = fmt.Sprintf("%s: %s", c.username(m.Author.ID), m.Content)
	}

	c.messages.Add(message)
}

func (c *Discord) username(id string) string {
	return strings.Title(strings.Split(c.users[id], "-")[0])
}

func (c *Discord) makeChatRequestWithMessages(messages []openai.ChatCompletionMessage) string {
	chatRequest := openai.ChatCompletionRequest{
		Model:       c.Model,
		Messages:    messages,
		Temperature: c.Temperature,
		TopP:        c.TopP,
	}
	resp, err := c.openai.CreateChatCompletion(context.Background(), chatRequest)

	e := &openai.APIError{}
	if errors.As(err, &e) {
		switch e.HTTPStatusCode {
		case 401:
			return fmt.Sprintf("invalid auth or key: %v\n", err)
		case 429:
			return fmt.Sprintf("rate limit exceeded: %v\n", err)
		case 500:
			return fmt.Sprintf("internal server error: %v\n", err)
		default:
			return fmt.Sprintf("unhandled error: %v\n", err)
		}
	}

	choices := resp.Choices
	if len(choices) < 1 {
		return ""
	}

	for _, choice := range choices {
		fmt.Printf("%s: %s\n", choice.FinishReason, choice.Message.Content)
	}

	response := choices[0].Message

	reply := response.Content

	// Sometimes the model prepends a name into its reply, like "Name:
	// hello" or "You: hello". This removes any prepended name from its
	// reply so that it looks natural on Discord.
	// fmt.Println("original reply:", reply)
	reply = regexp.MustCompile(`(?mi)(^[^: ]+:[ ]+)+`).ReplaceAllString(reply, "")

	return reply
}

func getRandom(m map[string]string) (string, string) {
	i := rand.Intn(len(m))
	for key, val := range m {
		if i == 0 {
			return key, val
		}
		i--
	}
	return "", ""
}
