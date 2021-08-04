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
// TODO implement "split" mode (input in thread, output in main channel)

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
			&cli.StringFlag{Name: "default-channel-mode", Aliases: []string{"m"}, EnvVars: []string{"REPLBOT_DEFAULT_CHANNEL_MODE"}, Value: config.DefaultChannelMode, DefaultText: config.DefaultChannelMode, Usage: "default channel mode (channel, thread or split)"},
			&cli.StringFlag{Name: "default-direct-mode", Aliases: []string{"M"}, EnvVars: []string{"REPLBOT_DEFAULT_DIRECT_MODE"}, Value: config.DefaultDirectMode, DefaultText: config.DefaultDirectMode, Usage: "default direct message mode (channel, thread or split)"},
		},
	}
}

func execRun(c *cli.Context) error {
	token := c.String("slack-token")
	scriptDir := c.String("script-dir")
	timeout := c.Duration("idle-timeout")
	defaultChannelMode := c.String("default-channel-mode")
	defaultDirectMode := c.String("default-direct-mode")
	if token == "" {
		return errors.New("missing Slack bot token, pass --slack-token or set REPLBOT_SLACK_BOT_TOKEN")
	} else if _, err := os.Stat(scriptDir); err != nil {
		return fmt.Errorf("cannot find REPL directory %s, set --repl-dir or set REPLBOT_REPL_DIR")
	} else if timeout < time.Minute {
		return fmt.Errorf("idle timeout has to be at least one minute")
	} else if _, err := os.ReadDir(scriptDir); err != nil {
		return fmt.Errorf("cannot read script directory: %s", err.Error())
	} else if defaultChannelMode != config.ModeChannel && defaultChannelMode != config.ModeThread && defaultChannelMode != config.ModeSplit {
		return errors.New("default channel mode must be 'channel', 'thread' or 'split'")
	} else if defaultDirectMode != config.ModeChannel && defaultDirectMode != config.ModeThread && defaultDirectMode != config.ModeSplit {
		return errors.New("default direct message mode must be 'channel', 'thread' or 'split'")
	}

	// Create main bot
	conf := config.New()
	conf.Token = token
	conf.ScriptDir = scriptDir
	conf.IdleTimeout = timeout
	conf.DefaultChannelMode = defaultChannelMode
	conf.DefaultDirectMode = defaultDirectMode
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
