package bot

import (
	"time"
)

type Format int

const (
	Markdown = iota
	Code
)

const (
	additionalRateLimitDuration = 500 * time.Millisecond
)

// TODO get rid of Sender interface, move to Conn
type Sender interface {
	Send(message string, format Format) error
	SendWithID(message string, format Format) (string, error)
	Update(id string, message string, format Format) error
}
