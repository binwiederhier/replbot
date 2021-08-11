package bot

import (
	"context"
	"heckel.io/replbot/config"
)

type ChannelType int

const (
	Unknown ChannelType = iota
	Channel
	DM
)

type Conn interface {
	Connect(ctx context.Context) (<-chan event, error)
	Sender(channel, threadTS string) Sender
	Mention() string
	SupportsMode(mode config.Mode) bool
	Close() error
}
