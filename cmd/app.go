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

// New creates a new CLI application
func New() *cli.App {
	flags := []cli.Flag{
		&cli.StringFlag{Name: "config", Aliases: []string{"c"}, EnvVars: []string{"REPLBOT_CONFIG_FILE"}, Value: "/etc/replbot/config.yml", DefaultText: "/etc/replbot/config.yml", Usage: "config file"},
		&cli.BoolFlag{Name: "debug", EnvVars: []string{"REPLBOT_DEBUG"}, Value: false, Usage: "enable debugging output"},
		altsrc.NewStringFlag(&cli.StringFlag{Name: "bot-token", Aliases: []string{"t"}, EnvVars: []string{"REPLBOT_BOT_TOKEN"}, DefaultText: "none", Usage: "bot token"}),
		altsrc.NewStringFlag(&cli.StringFlag{Name: "script-dir", Aliases: []string{"d"}, EnvVars: []string{"REPLBOT_SCRIPT_DIR"}, Value: "/etc/replbot/script.d", DefaultText: "/etc/replbot/script.d", Usage: "script directory"}),
		altsrc.NewDurationFlag(&cli.DurationFlag{Name: "idle-timeout", Aliases: []string{"T"}, EnvVars: []string{"REPLBOT_IDLE_TIMEOUT"}, Value: config.DefaultIdleTimeout, Usage: "timeout after which sessions are ended"}),
		altsrc.NewStringFlag(&cli.StringFlag{Name: "default-control-mode", Aliases: []string{"m"}, EnvVars: []string{"REPLBOT_DEFAULT_CONTROL_MODE"}, Value: string(config.DefaultControlMode), DefaultText: string(config.DefaultControlMode), Usage: "default control mode [channel, thread or split]"}),
		altsrc.NewStringFlag(&cli.StringFlag{Name: "default-window-mode", Aliases: []string{"w"}, EnvVars: []string{"REPLBOT_DEFAULT_WINDOW_MODE"}, Value: string(config.DefaultWindowMode), DefaultText: string(config.DefaultWindowMode), Usage: "default window mode [full or trim]"}),
		altsrc.NewStringFlag(&cli.StringFlag{Name: "default-auth-mode", Aliases: []string{"a"}, EnvVars: []string{"REPLBOT_DEFAULT_AUTH_MODE"}, Value: string(config.DefaultAuthMode), DefaultText: string(config.DefaultAuthMode), Usage: "default auth mode [only-me or everyone]"}),
		altsrc.NewStringFlag(&cli.StringFlag{Name: "default-size", Aliases: []string{"s"}, EnvVars: []string{"REPLBOT_DEFAULT_SIZE"}, Value: config.DefaultSize.Name, DefaultText: config.DefaultSize.Name, Usage: "default terminal size [tiny, small, medium, or large]"}),
		altsrc.NewBoolFlag(&cli.BoolFlag{Name: "default-record", Aliases: []string{"r"}, EnvVars: []string{"REPLBOT_DEFAULT_RECORD"}, Usage: "record sessions by default"}),
		altsrc.NewBoolFlag(&cli.BoolFlag{Name: "no-default-record", Aliases: []string{"R"}, EnvVars: []string{"REPLBOT_NO_DEFAULT_RECORD"}, Usage: "do not record sessions by default"}),
		altsrc.NewStringFlag(&cli.StringFlag{Name: "cursor", Aliases: []string{"C"}, EnvVars: []string{"REPLBOT_CURSOR"}, Value: "on", Usage: "cursor blink rate (on, off or duration)"}),
		altsrc.NewStringFlag(&cli.StringFlag{Name: "share-host", Aliases: []string{"H"}, EnvVars: []string{"REPLBOT_SHARE_HOST"}, Usage: "SSH hostname:port, used for terminal sharing"}),
		altsrc.NewStringFlag(&cli.StringFlag{Name: "share-key-file", Aliases: []string{"K"}, EnvVars: []string{"REPLBOT_SHARE_KEY_FILE"}, Value: "/etc/replbot/hostkey", Usage: "SSH host key file, used for terminal sharing"}),
	}
	return &cli.App{
		Name:                   "replbot",
		Usage:                  "Slack/Discord bot for running interactive REPLs and shells from a chat",
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
	token := c.String("bot-token")
	scriptDir := c.String("script-dir")
	timeout := c.Duration("idle-timeout")
	defaultControlMode := config.ControlMode(c.String("default-control-mode"))
	defaultWindowMode := config.WindowMode(c.String("default-window-mode"))
	defaultAuthMode := config.AuthMode(c.String("default-auth-mode"))
	cursor := c.String("cursor")
	shareHost := c.String("share-host")
	shareKeyFile := c.String("share-key-file")
	debug := c.Bool("debug")
	if token == "" || token == "MUST_BE_SET" {
		return errors.New("missing bot token, pass --bot-token, set REPLBOT_BOT_TOKEN env variable or bot-token config option")
	} else if _, err := os.Stat(scriptDir); err != nil {
		return fmt.Errorf("cannot find REPL directory %s, set --script-dir, set REPLBOT_SCRIPT_DIR env variable, or script-dir config option", scriptDir)
	} else if timeout < time.Minute {
		return fmt.Errorf("idle timeout has to be at least one minute")
	} else if entries, err := os.ReadDir(scriptDir); err != nil || len(entries) == 0 {
		return errors.New("cannot read script directory, or directory empty")
	} else if defaultControlMode != config.Channel && defaultControlMode != config.Thread && defaultControlMode != config.Split {
		return errors.New("default mode must be 'channel', 'thread' or 'split'")
	} else if defaultWindowMode != config.Full && defaultWindowMode != config.Trim {
		return errors.New("default window mode must be 'full' or 'trim'")
	} else if defaultAuthMode != config.OnlyMe && defaultAuthMode != config.Everyone {
		return errors.New("default window mode must be 'full' or 'trim'")
	} else if shareHost != "" && (shareKeyFile == "" || !util.FileExists(shareKeyFile)) {
		return errors.New("share key file must be set and exist if share host is set, check --share-key-file or REPLBOT_SHARE_KEY_FILE")
	}
	cursorRate, err := parseCursorRate(cursor)
	if err != nil {
		return err
	}
	defaultSize, err := config.ParseSize(c.String("default-size"))
	if err != nil {
		return err
	}
	var defaultRecord bool
	if c.IsSet("no-default-record") {
		defaultRecord = false
	} else if c.IsSet("default-record") {
		defaultRecord = true
	} else {
		defaultRecord = config.DefaultRecord
	}

	// Create main bot
	conf := config.New(token)
	conf.ScriptDir = scriptDir
	conf.IdleTimeout = timeout
	conf.DefaultControlMode = defaultControlMode
	conf.DefaultWindowMode = defaultWindowMode
	conf.DefaultAuthMode = defaultAuthMode
	conf.DefaultSize = defaultSize
	conf.DefaultRecord = defaultRecord
	conf.Cursor = cursorRate
	conf.ShareHost = shareHost
	conf.ShareKeyFile = shareKeyFile
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
	if err := robot.Run(); err != nil {
		return err
	}

	log.Printf("Exiting.")
	return nil
}

func parseCursorRate(cursor string) (time.Duration, error) {
	switch cursor {
	case "on":
		return config.CursorOn, nil
	case "off":
		return config.CursorOff, nil
	default:
		cursorRate, err := time.ParseDuration(cursor)
		if err != nil {
			return 0, err
		} else if cursorRate < 500*time.Millisecond {
			return 0, fmt.Errorf("cursor rate is too low, min allowed is 500ms, though that'll probably cause rate limiting issues too")
		} else if cursorRate < time.Second {
			log.Printf("warning: cursor rate is really low; we'll get rate limited if there are too many shells open")
		}
		return cursorRate, nil
	}
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
