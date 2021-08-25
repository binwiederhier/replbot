package bot

import (
	"context"
	"io"
)

type channelID struct {
	Channel string
	Thread  string
}

type conn interface {
	Connect(ctx context.Context) (<-chan event, error)
	Send(channel *channelID, message string) error
	SendWithID(channel *channelID, message string) (string, error)
	UploadFile(channel *channelID, message string, filename string, filetype string, file io.Reader) error
	Update(channel *channelID, id string, message string) error
	Archive(channel *channelID) error
	MentionBot() string
	Mention(user string) string
	ParseMention(user string) (string, error)
	Unescape(s string) string
	Close() error
}
