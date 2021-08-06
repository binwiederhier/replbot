package config

import (
	"os"
	"path/filepath"
	"time"
)

const (
	// DefaultIdleTimeout defines the default time after which a session is terminated
	DefaultIdleTimeout = 10 * time.Minute

	// DefaultMode defines the default window mode in which sessions are controlled
	DefaultMode = ModeSplit

	// ModeThread is the mode constant to define that both terminal window and user control appear in a thread
	ModeThread = "thread"

	// ModeChannel is the mode constant to define that both terminal window and user control appear in a channel
	ModeChannel = "channel"

	// ModeSplit is the mode constant to define that the terminal window is displayed in the main channel, and the user input from a thread
	ModeSplit = "split"
)

// Config is the main config struct for the application. Use New to instantiate a default config struct.
type Config struct {
	Token       string
	ScriptDir   string
	IdleTimeout time.Duration
	DefaultMode string
}

// New instantiates a default new config
func New() *Config {
	return &Config{
		IdleTimeout: DefaultIdleTimeout,
		DefaultMode: DefaultMode,
	}
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
