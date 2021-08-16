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
)

// ControlMode defines where the control channel and where the terminal will be
// opened, see config.yml for details
type ControlMode string

const (
	DefaultControlMode = Split
	Thread             = ControlMode("thread")
	Channel            = ControlMode("channel")
	Split              = ControlMode("split")
)

type WindowMode string

const (
	DefaultWindowMode = Full
	Full              = WindowMode("full")
	Trim              = WindowMode("trim")
)

// Size defines the dimensions of the terminal
type Size struct {
	Name   string
	Width  int
	Height int
}

var (
	Tiny   = &Size{"tiny", 60, 15}
	Small  = &Size{"small", 80, 24}
	Medium = &Size{"medium", 100, 30}
	Large  = &Size{"large", 120, 38}

	DefaultSize = Small
	Sizes       = map[string]*Size{
		Tiny.Name:   Tiny,
		Small.Name:  Small,
		Medium.Name: Medium,
		Large.Name:  Large,
	}
)

const (
	CursorOff = time.Duration(0)
	CursorOn  = time.Duration(1)
)

const (
	TypeSlack   = "slack"
	TypeDiscord = "discord"
)

// Config is the main config struct for the application. Use New to instantiate a default config struct.
type Config struct {
	Token              string
	ScriptDir          string
	IdleTimeout        time.Duration
	DefaultControlMode ControlMode
	DefaultWindowMode  WindowMode
	DefaultSize        *Size
	Cursor             time.Duration
	ShareHost          string
	Debug              bool
}

// New instantiates a default new config
func New(token string) *Config {
	return &Config{
		Token:              token,
		IdleTimeout:        DefaultIdleTimeout,
		DefaultControlMode: DefaultControlMode,
		DefaultWindowMode:  DefaultWindowMode,
		DefaultSize:        DefaultSize,
	}
}

func (c *Config) Type() string {
	if strings.HasPrefix(c.Token, "xoxb-") {
		return TypeSlack
	}
	return TypeDiscord
}

func (c *Config) ShareEnabled() bool {
	return c.ShareHost != ""
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
