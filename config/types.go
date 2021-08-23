package config

import (
	"time"
)

// Platform defines the target chat application platform
type Platform string

// All possible Platform constants
const (
	Slack   = Platform("slack")
	Discord = Platform("discord")
)

// ControlMode defines where the control channel and where the terminal will be
// opened, see config.yml for details
type ControlMode string

// All possible ControlMode constants
const (
	DefaultControlMode = Split
	Thread             = ControlMode("thread")
	Channel            = ControlMode("channel")
	Split              = ControlMode("split")
)

// WindowMode defines whether white spaces are trimmed from the terminal
type WindowMode string

// All possible WindowMode constants
const (
	DefaultWindowMode = Full
	Full              = WindowMode("full")
	Trim              = WindowMode("trim")
)

// AuthMode defines who is allowed to interact with the session by default
type AuthMode string

// All possible AuthMode constants
const (
	DefaultAuthMode = Everyone
	OnlyMe          = AuthMode("only-me")
	Everyone        = AuthMode("everyone")
)

// Size defines the dimensions of the terminal
type Size struct {
	Name   string
	Width  int
	Height int
}

// All possible Size constants
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

// Constants used to toggle the cursor on or off
const (
	CursorOff = time.Duration(0)
	CursorOn  = time.Duration(1)
)
