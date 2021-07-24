package config

import "time"

const (
	DefaultIdleTimeout = 10 * time.Minute
)

type Config struct {
	Token       string
	ScriptDir   string
	IdleTimeout time.Duration
}
