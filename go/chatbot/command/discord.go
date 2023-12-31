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
	"strings"
	"syscall"
	"time"

	"github.com/andreykaipov/discord-bots/go/chatbot/pkg"
	"github.com/bwmarrin/discordgo"
	openai "github.com/sashabaranov/go-openai"
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
	PromptFile   *os.File `required:"" name:"prompt" env:"PROMPT_FILE"`
	UsersFile    *os.File `required:"" name:"users" env:"USERS_FILE"`
	users        map[string]string
	openai       *openai.Client

	MessageContext             int          `optional:"" default:"20" help:"The number of previous messages to send back to OpenAI"`
	MessageContextInterval     int          `optional:"" default:"90" help:"The time in seconds until previous message context is reset, if no new messages are received"`
	MessageReplyInterval       int          `optional:"" default:"1" help:"The base time in seconds after a message is received to wait before sending a reply"`
	MessageReplyIntervalRandom int          `optional:"" default:"5" help:"The time in seconds to add to the base message reply interval"`
	MessageReplyDoubleChance   int          `optional:"" default:"10" help:"The percent chance that the bot will reply twice in a row"`
	messageReplyTicker         *time.Ticker // interval for determining message reply
	messageContextTicker       *time.Ticker // interval for resetting message context
	messages                   *pkg.LimitedQueue[openai.ChatCompletionMessage]
	replying                   bool
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

	prompt, err := io.ReadAll(c.PromptFile)
	if err != nil {
		return err
	}
	c.messages.AddSticky(openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: string(prompt),
	})

	users, err := io.ReadAll(c.UsersFile)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(users, &c.users); err != nil {
		return err
	}

	return nil
}

func (c *Discord) Run() error {
	fmt.Println("Bot is now running. Press CTRL-C to exit.")

	defer func() {
		_ = c.discord.Close()
		if _, err := c.discord.ChannelMessageSend(c.ChatChannel, "Bye..."); err != nil {
			fmt.Printf("error sending message: %v\n", err)
		}
	}()

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

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
			c.Kong.Printf("resetting limited queue")
			c.messages.ClearNonSticky()
		case <-sc:
			return nil
		}
	}
}

func (c *Discord) resetMessageTickers() {
	c.messageReplyTicker.Reset(time.Duration(c.MessageReplyInterval+rand.Intn(c.MessageReplyIntervalRandom)) * time.Second)
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
		if c.messages.LastN(2).Role == openai.ChatMessageRoleAssistant || rand.Intn(100) < 100-c.MessageReplyDoubleChance {
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
