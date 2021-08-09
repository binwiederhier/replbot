// Package cmd provides the replbot CLI application
package cmd

import (
	"errors"
	"fmt"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
	"heckel.io/replbot/bot"
	"heckel.io/replbot/config"
	"heckel.io/replbot/util"
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
	flags := []cli.Flag{
		&cli.StringFlag{Name: "config", Aliases: []string{"c"}, EnvVars: []string{"REPLBOT_CONFIG_FILE"}, Value: "/etc/replbot/config.yml", DefaultText: "/etc/replbot/config.yml", Usage: "config file"},
		&cli.BoolFlag{Name: "debug", EnvVars: []string{"REPLBOT_DEBUG"}, Value: false, Usage: "enable debugging output"},
		altsrc.NewStringFlag(&cli.StringFlag{Name: "slack-bot-token", Aliases: []string{"t"}, EnvVars: []string{"REPLBOT_SLACK_BOT_TOKEN"}, DefaultText: "none", Usage: "Slack bot token"}),
		altsrc.NewStringFlag(&cli.StringFlag{Name: "script-dir", Aliases: []string{"d"}, EnvVars: []string{"REPLBOT_SCRIPT_DIR"}, Value: "/etc/replbot/script.d", DefaultText: "/etc/replbot/script.d", Usage: "script directory"}),
		altsrc.NewDurationFlag(&cli.DurationFlag{Name: "idle-timeout", Aliases: []string{"T"}, EnvVars: []string{"REPLBOT_IDLE_TIMEOUT"}, Value: config.DefaultIdleTimeout, Usage: "timeout after which sessions are ended"}),
		altsrc.NewStringFlag(&cli.StringFlag{Name: "default-mode", Aliases: []string{"m"}, EnvVars: []string{"REPLBOT_DEFAULT_MODE"}, Value: config.DefaultMode, DefaultText: config.DefaultMode, Usage: "default mode [channel, thread or split]"}),
		altsrc.NewDurationFlag(&cli.DurationFlag{Name: "cursor-rate", Aliases: []string{"C"}, EnvVars: []string{"REPLBOT_CURSOR_RATE"}, Value: config.DefaultCursorRate, Usage: "cursor blink rate; too low values cause rate limiting issues"}),
	}
	return &cli.App{
		Name:                   "replbot",
		Usage:                  "Slack bot that provides interactive REPLs",
		UsageText:              "replbot [OPTION..]",
		HideHelp:               true,
		HideVersion:            true,
		EnableBashCompletion:   true,
		UseShortOptionHandling: true,
		Reader:                 os.Stdin,
		Writer:                 os.Stdout,
		ErrWriter:              os.Stderr,
		Action:                 execRun,
		Before:                 initConfigFileInputSource("config", flags),
		Flags:                  flags,
	}
}

func execRun(c *cli.Context) error {
	token := c.String("slack-bot-token")
	scriptDir := c.String("script-dir")
	timeout := c.Duration("idle-timeout")
	defaultMode := c.String("default-mode")
	cursorRate := c.Duration("cursor-rate")
	debug := c.Bool("debug")
	if token == "" || token == "MUST_BE_SET" {
		return errors.New("missing bot token, pass --slack-bot-token, set REPLBOT_SLACK_BOT_TOKEN env variable or slack-bot-token config option")
	} else if _, err := os.Stat(scriptDir); err != nil {
		return fmt.Errorf("cannot find REPL directory %s, set --script-dir, set REPLBOT_SCRIPT_DIR env variable, or script-dir config option", scriptDir)
	} else if timeout < time.Minute {
		return fmt.Errorf("idle timeout has to be at least one minute")
	} else if entries, err := os.ReadDir(scriptDir); err != nil || len(entries) == 0 {
		return errors.New("cannot read script directory, or directory empty")
	} else if defaultMode != config.ModeChannel && defaultMode != config.ModeThread && defaultMode != config.ModeSplit {
		return errors.New("default mode must be 'channel', 'thread' or 'split'")
	} else if cursorRate < 500*time.Millisecond {
		return fmt.Errorf("cursor rate is too low, min allowed is 500ms, though that'll probably cause rate limiting issues too")
	} else if cursorRate < time.Second {
		log.Printf("warning: cursor rate is really low; Slack will rate limit us if there are too many shells open")
	}

	// Create main bot
	conf := config.New()
	conf.Token = token
	conf.ScriptDir = scriptDir
	conf.IdleTimeout = timeout
	conf.DefaultMode = defaultMode
	conf.CursorRate = cursorRate
	conf.Debug = debug
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

// initConfigFileInputSource is like altsrc.InitInputSourceWithContext and altsrc.NewYamlSourceFromFlagFunc, but checks
// if the config flag is exists and only loads it if it does. If the flag is set and the file exists, it fails.
func initConfigFileInputSource(configFlag string, flags []cli.Flag) cli.BeforeFunc {
	return func(context *cli.Context) error {
		configFile := context.String(configFlag)
		if context.IsSet(configFlag) && !util.FileExists(configFile) {
			return fmt.Errorf("config file %s does not exist", configFile)
		} else if !context.IsSet(configFlag) && !util.FileExists(configFile) {
			return nil
		}
		inputSource, err := altsrc.NewYamlSourceFromFile(configFile)
		if err != nil {
			return err
		}
		return altsrc.ApplyInputSourceValues(context, inputSource, flags)
	}
}
