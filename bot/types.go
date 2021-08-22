package bot

type event interface{}

type messageEvent struct {
	ID          string
	Channel     string
	ChannelType channelType
	Thread      string
	User        string
	Message     string
}

type channelJoinedEvent struct {
	Channel string
}

type errorEvent struct {
	Error error
}

type channelType int

const (
	channelTypeUnknown channelType = iota
	channelTypeChannel
	channelTypeDM
)
