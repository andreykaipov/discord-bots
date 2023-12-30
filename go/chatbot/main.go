package main

import (
	"github.com/alecthomas/kong"
	"github.com/andreykaipov/discord-bots/go/chatbot/command"
)

type cli struct {
	command.Context
	Discord command.Discord `cmd:"" help:"Start the Discord bot."`
}

func main() {
	ctx := kong.Parse(
		&cli{},
		kong.Name("chatbot"),
		kong.Description("ChatGPT Discord chatbot"),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
	)
	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}
