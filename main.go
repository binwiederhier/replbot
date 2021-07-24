package main

import (
	"os"
)

func main() {
	// TODO add readme
	// TODO CLI
	// TODO say goodbye to sessions when ctrl-c on main program
	config := &Config{
		Token:     os.Getenv("SLACK_BOT_TOKEN"),
		ScriptDir: "repls.d",
	}
	bot, err := NewBot(config)
	if err != nil {
		panic(err)
	}
	if err := bot.Start(); err != nil {
		panic(err)
	}
}
