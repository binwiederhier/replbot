package main

import (
	"os"
)

func main() {
	config := &Config{
		Token: os.Getenv("SLACK_BOT_TOKEN"),
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
