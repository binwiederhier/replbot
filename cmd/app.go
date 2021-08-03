// Package cmd provides the replbot CLI application
package cmd

import (
	"errors"
	"fmt"
	"github.com/urfave/cli/v2"
	"heckel.io/replbot/bot"
	"heckel.io/replbot/config"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// TODO xterm.js URL
// TODO make "thread" mode split into 3-4 messages

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
			&cli.StringFlag{Name: "script-dir", Aliases: []string{"d"}, EnvVars: []string{"REPLBOT_SCRIPT_DIR"}, Value: "script.d", DefaultText: "script.d", Usage: "script directory"},
			&cli.DurationFlag{Name: "idle-timeout", Aliases: []string{"T"}, EnvVars: []string{"REPLBOT_IDLE_TIMEOUT"}, Value: config.DefaultIdleTimeout, Usage: "timeout after which sessions are ended"},
			&cli.StringFlag{Name: "default-mode", Aliases: []string{"m"}, EnvVars: []string{"REPLBOT_DEFAULT_MODE"}, Value: config.DefaultMode, DefaultText: config.DefaultMode, Usage: "default REPL mode (channel or thread)"},
		},
	}
}

func execRun(c *cli.Context) error {
	token := c.String("slack-token")
	scriptDir := c.String("script-dir")
	timeout := c.Duration("idle-timeout")
	defaultMode := c.String("default-mode")
	if token == "" {
		return errors.New("missing Slack bot token, pass --slack-token or set REPLBOT_SLACK_BOT_TOKEN")
	} else if _, err := os.Stat(scriptDir); err != nil {
		return fmt.Errorf("cannot find REPL directory %s, set --repl-dir or set REPLBOT_REPL_DIR")
	} else if timeout < time.Minute {
		return fmt.Errorf("idle timeout has to be at least one minute")
	} else if _, err := os.ReadDir(scriptDir); err != nil {
		return fmt.Errorf("cannot read script directory: %s", err.Error())
	} else if defaultMode != config.ModeChannel && defaultMode != config.ModeThread {
		return errors.New("default mode must be either 'channel' or 'thread'")
	}

	// Create main bot
	conf := config.New()
	conf.Token = token
	conf.ScriptDir = scriptDir
	conf.IdleTimeout = timeout
	conf.DefaultMode = defaultMode
	robot, err := bot.New(conf)
	if err != nil {
		return err
	}

	// Set up signal handling
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs // Doesn't matter which
		log.Printf("Signal received. Closing all active sessions.")
		robot.Stop()
	}()

	// Run main bot, can be killed by signal
	if err := robot.Start(); err != nil {
		return err
	}
	log.Printf("Exiting.")
	return nil
}
