// Package cmd provides the replbot CLI application
package cmd

import (
	"errors"
	"fmt"
	"github.com/urfave/cli/v2"
	"heckel.io/replbot/bot"
	"heckel.io/replbot/config"
	"os"
)

// New creates a new CLI application
func New() *cli.App {
	return &cli.App{
		Name:                   "replbot",
		Usage:                  "Slack bot that provides interactive REPLs",
		UsageText:              "replbot [OPTION..] [ARG..]",
		HideHelp:               true,
		HideVersion:            true,
		EnableBashCompletion:   true,
		UseShortOptionHandling: true,
		Reader:                 os.Stdin,
		Writer:                 os.Stdout,
		ErrWriter:              os.Stderr,
		Action:                 execRun,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "slack-token", Aliases: []string{"t"}, EnvVars: []string{"REPLBOT_SLACK_BOT_TOKEN"}, DefaultText: "none", Usage: "bot token for the Slack app"},
			&cli.StringFlag{Name: "repl-dir", Aliases: []string{"d"}, EnvVars: []string{"REPLBOT_REPL_DIR"}, Value: "repls.d", DefaultText: "repls.d", Usage: "script directory"},
		},
	}
}

func execRun(c *cli.Context) error {
	token := c.String("slack-token")
	dir := c.String("repl-dir")
	if token == "" {
		return errors.New("missing Slack bot token, pass --slack-token or set REPLBOT_SLACK_BOT_TOKEN")
	} else if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("cannot find REPL directory %s, set --repl-dir or set REPLBOT_REPL_DIR")
	}
	robot, err := bot.New(&config.Config{
		Token:     token,
		ScriptDir: dir,
	})
	if err != nil {
		return err
	}
	if err := robot.Start(); err != nil {
		return err
	}
	return nil
}
