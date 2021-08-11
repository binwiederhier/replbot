package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultIdleTimeout defines the default time after which a session is terminated
	DefaultIdleTimeout = 10 * time.Minute

	// DefaultMode defines the default window mode in which sessions are controlled
	DefaultMode = ModeSplit

	DefaultSize = SizeSmall
)

type Mode string

const (
	// ModeThread is the mode constant to define that both terminal window and user control appear in a thread
	ModeThread = Mode("thread")

	// ModeChannel is the mode constant to define that both terminal window and user control appear in a channel
	ModeChannel = Mode("channel")

	// ModeSplit is the mode constant to define that the terminal window is displayed in the main channel, and the user input from a thread
	ModeSplit = Mode("split")
)

// Predefined terminal sizes
const (
	SizeTiny   = "tiny"
	SizeSmall  = "small"
	SizeMedium = "medium"
	SizeLarge  = "large"
	SizeMax    = "max"
)

const (
	CursorOff = time.Duration(0)
	CursorOn  = time.Duration(1)
)

const (
	TypeSlack   = "slack"
	TypeDiscord = "discord"
)

var (
	Sizes = map[string][2]int{
		SizeTiny:   {60, 15},
		SizeSmall:  {80, 24},
		SizeMedium: {100, 30},
		SizeLarge:  {120, 38},
		SizeMax:    {150, 50},
	}
	MinSize = Sizes[SizeTiny]
	MaxSize = Sizes[SizeMax]
)

// Config is the main config struct for the application. Use New to instantiate a default config struct.
type Config struct {
	Token       string
	ScriptDir   string
	IdleTimeout time.Duration
	DefaultMode Mode
	DefaultSize string
	Cursor      time.Duration
	Debug       bool
}

// New instantiates a default new config
func New(token string) *Config {
	return &Config{
		Token:       token,
		IdleTimeout: DefaultIdleTimeout,
		DefaultMode: DefaultMode,
		DefaultSize: DefaultSize,
	}
}

func (c *Config) Type() string {
	if strings.HasPrefix(c.Token, "xoxb-") {
		return TypeSlack
	}
	return TypeDiscord
}

// Scripts returns the names of all available scripts
func (c *Config) Scripts() []string {
	scripts := make([]string, 0)
	for script := range c.scripts() {
		scripts = append(scripts, script)
	}
	return scripts
}

// Script returns the path to the script with the given name.
// If a script with the given name does not exist, the result may be empty.
func (c *Config) Script(name string) string {
	scripts := c.scripts()
	return scripts[name]
}

func (c *Config) scripts() map[string]string {
	scripts := make(map[string]string)
	entries, err := os.ReadDir(c.ScriptDir)
	if err != nil {
		return scripts
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			scripts[entry.Name()] = filepath.Join(c.ScriptDir, entry.Name())
		}
	}
	return scripts
}
