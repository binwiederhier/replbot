package config

import (
	"os"
	"path/filepath"
	"time"
)

const (
	DefaultIdleTimeout = 10 * time.Minute
	DefaultMode        = ModeChannel
	ModeThread         = "thread"
	ModeChannel        = "channel"
)

type Config struct {
	Token       string
	ScriptDir   string
	IdleTimeout time.Duration
	DefaultMode string
}

func New() *Config {
	return &Config{
		IdleTimeout: DefaultIdleTimeout,
		DefaultMode: ModeThread,
	}
}

func (c *Config) Scripts() []string {
	scripts := make([]string, 0)
	for script, _ := range c.scripts() {
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
	scripts := make(map[string]string, 0)
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
