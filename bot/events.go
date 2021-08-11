package bot

type event interface{}

type messageEvent struct {
	ID          string
	Channel     string
	ChannelType ChannelType
	Thread      string
	User        string
	Message     string
}

type channelJoined struct {
	Channel string
}

type errorEvent struct {
	Error error
}
