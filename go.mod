module heckel.io/replbot

go 1.16

require (
	github.com/BurntSushi/toml v0.4.1 // indirect
	github.com/bwmarrin/discordgo v0.23.3-0.20210811014036-f7454d039f3a
	github.com/cpuguy83/go-md2man/v2 v2.0.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/slack-go/slack v0.9.4
	github.com/urfave/cli/v2 v2.3.0
	golang.org/x/crypto v0.0.0-20210711020723-a769d52b0f97 // indirect
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/sys v0.0.0-20210809222454-d867a43fc93e // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)

replace github.com/bwmarrin/discordgo => github.com/Pedro-Pessoa/discordgo v0.23.3-0.20210809044142-3ac6a06d9699
