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
	"github.com/sashabaranov/go-openai/jsonschema"
)

type Discord struct {
	Command
	DiscordToken           string   `required:"" env:"DISCORD_TOKEN"`
	OpenAIAPIKey           string   `name:"openai-api-key" required:"" env:"OPENAI_API_KEY"`
	Model                  string   `optional:"" name:"model"`
	PromptFile             *os.File `required:"" name:"prompt"`
	UsersFile              *os.File `required:"" name:"users"`
	Temperature            float32  `optional:"" default:"1"`
	TopP                   float32  `optional:"" default:"1"`
	Channel                string   `required:"" help:"A channel ID to reply in"`
	PreviousMessageContext int      `optional:""  default:"10"`
	Tools                  bool     `optional:"" default:"false"`

	discord              *discordgo.Session
	users                map[string]string
	paused               bool
	messageReplyTicker   *time.Ticker // interval for determining message reply
	messageContextTicker *time.Ticker // interval for resetting message context

	openai   *openai.Client
	messages *pkg.LimitedQueue[openai.ChatCompletionMessage]
	tools    []openai.Tool
}

func (c *Discord) AfterApply() error {
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
		c.messages = pkg.NewLimitedQueue[openai.ChatCompletionMessage](c.PreviousMessageContext)
	}

	if c.Tools {
		params := jsonschema.Definition{
			Type: jsonschema.Object,
			Properties: map[string]jsonschema.Definition{
				"location": {
					Type:        jsonschema.String,
					Description: "The city and state, e.g. San Francisco, CA",
				},
				"unit": {
					Type: jsonschema.String,
					Enum: []string{"celsius", "fahrenheit"},
				},
			},
			Required: []string{"location"},
		}
		f := openai.FunctionDefinition{
			Name:        "get_current_weather",
			Description: "Get the current weather in a given location",
			Parameters:  params,
		}
		t := openai.Tool{
			Type:     openai.ToolTypeFunction,
			Function: f,
		}
		c.tools = append(c.tools, t)
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
		if _, err := c.discord.ChannelMessageSend(c.Channel, "Bye..."); err != nil {
			fmt.Printf("error sending message: %v\n", err)
		}
	}()

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	c.resetMessageReplyTicker()
	defer c.messageReplyTicker.Stop()
	defer c.messageContextTicker.Stop()

	for {
		select {
		case <-c.messageReplyTicker.C:
			c.attemptSendReply(c.Channel)
		case <-c.messageContextTicker.C:
			if !c.paused || len(c.messages.Items()) == 0 {
				continue
			}
			c.Kong.Printf("resetting limited queue")
			c.messages = pkg.NewLimitedQueue[openai.ChatCompletionMessage](c.PreviousMessageContext)
		case <-sc:
			return nil
		}
	}
}

func (c *Discord) resetMessageReplyTicker() {
	c.messageReplyTicker.Reset(time.Duration(3+rand.Intn(3)) * time.Second)
	c.messageContextTicker.Reset(time.Duration(2) * time.Minute)
	c.paused = false
}

func (c *Discord) attemptSendReply(channel string) {
	// don't reply if there are no messages
	if len(c.messages.Items()) == 0 {
		c.messageReplyTicker.Stop()
		c.paused = true
		return
	}

	// don't reply to yourself
	if c.messages.Last().Role == openai.ChatMessageRoleAssistant {
		c.messageReplyTicker.Stop()
		c.paused = true
		return
	}

	_ = c.discord.ChannelTyping(channel)
	reply := c.makeChatRequestWithMessages(c.messages.AllItems())

	if _, err := c.discord.ChannelMessageSend(channel, reply); err != nil {
		fmt.Printf("error sending message: %v\n", err)
	}
}

func (c *Discord) username(id string) string {
	return strings.Title(strings.Split(c.users[id], "-")[0])
}

func (c *Discord) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.ChannelID != c.Channel {
		return
	}
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
		c.resetMessageReplyTicker()
		c.discord.ChannelTyping(m.ChannelID)
		message.Role = openai.ChatMessageRoleUser
		message.Content = fmt.Sprintf("%s: %s", c.username(m.Author.ID), m.Content)
	}

	fmt.Println(message.Content)
	c.messages.Add(message)
}

func (c *Discord) makeChatRequestWithMessages(messages []openai.ChatCompletionMessage) string {
	chatRequest := openai.ChatCompletionRequest{
		Model:       c.Model,
		Messages:    messages,
		Temperature: c.Temperature,
		TopP:        c.TopP,
	}
	if c.Tools {
		chatRequest.Tools = c.tools
		chatRequest.ToolChoice = "auto"
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

	response := choices[0].Message
	if len(response.ToolCalls) > 0 {
		call := response.ToolCalls[0]
		return fmt.Sprintf("%v", call.Function)
	}

	reply := response.Content

	// Sometimes the model prepends a name into its reply, like "Name:
	// hello" or "You: hello". This removes any prepended name from its
	// reply so that it looks natural on Discord.
	// fmt.Println("original reply:", reply)
	reply = regexp.MustCompile(`(?mi)(^[^: ]+:[ ]+)+`).ReplaceAllString(reply, "")

	return reply
}