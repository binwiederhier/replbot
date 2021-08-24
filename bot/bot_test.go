package bot

import (
	"github.com/stretchr/testify/assert"
	"heckel.io/replbot/config"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	maxWaitTime = 5 * time.Second
)

var (
	testScripts = map[string]string{
		"enter-name": `
#!/bin/bash
case "$1" in
  run)
    while true; do
      echo -n "Enter name: "
      read name
      echo "Hello $name!"
    done 
    ;;
  *) ;;
esac
`,
		"bash": `
#!/bin/bash
case "$1" in
  run) bash -i ;;
  *) ;;
esac
`,
	}
)

func TestBotIgnoreNonMentionsAndShowHelpMessage(t *testing.T) {
	conf := createConfig(t)
	robot, err := New(conf)
	if err != nil {
		t.Fatal(err)
	}
	go robot.Run()
	defer robot.Stop()
	conn := robot.conn.(*memConn)

	conn.Event(&messageEvent{
		ID:          "user-1",
		Channel:     "channel",
		ChannelType: channelTypeChannel,
		Thread:      "",
		User:        "phil",
		Message:     "This message should be ignored",
	})
	conn.Event(&messageEvent{
		ID:          "user-2",
		Channel:     "channel",
		ChannelType: channelTypeChannel,
		Thread:      "",
		User:        "phil",
		Message:     "@replbot",
	})
	assert.True(t, conn.MessageContainsWait("1", "Hi there"))
	assert.True(t, conn.MessageContainsWait("1", "I'm a robot for running interactive REPLs"))
	assert.NotContains(t, conn.Message("1").Message, "This message should be ignored")
}

func TestBotBashSplitMode(t *testing.T) {
	conf := createConfig(t)
	robot, err := New(conf)
	if err != nil {
		t.Fatal(err)
	}
	go robot.Run()
	defer robot.Stop()
	conn := robot.conn.(*memConn)

	conn.Event(&messageEvent{
		ID:          "user-1",
		Channel:     "channel",
		ChannelType: channelTypeChannel,
		Thread:      "",
		User:        "phil",
		Message:     "@replbot bash",
	})
	assert.True(t, conn.MessageContainsWait("1", "REPL session started, @phil"))
	assert.True(t, conn.MessageContainsWait("2", "$")) // this is stupid ...

	conn.Event(&messageEvent{
		ID:          "user-2",
		Channel:     "channel",
		ChannelType: channelTypeChannel,
		Thread:      "user-1", // split mode!
		User:        "phil",
		Message:     "!e echo Phil\\bL was here",
	})
	assert.True(t, conn.MessageContainsWait("2", "PhiL was here"))
}

func TestBotBashDMChannelOnlyMeAllowDeny(t *testing.T) {
	conf := createConfig(t)
	robot, err := New(conf)
	if err != nil {
		t.Fatal(err)
	}
	go robot.Run()
	defer robot.Stop()
	conn := robot.conn.(*memConn)

	// Start in channel mode with "only-me"
	conn.Event(&messageEvent{
		ID:          "user-1",
		Channel:     "some-dm",
		ChannelType: channelTypeDM,
		Thread:      "",
		User:        "phil",
		Message:     "bash only-me channel", // no mention, because DM!
	})
	assert.True(t, conn.MessageContainsWait("1", "REPL session started, @phil"))
	assert.True(t, conn.MessageContainsWait("2", "$")) // this is stupid ...

	// Send message from someone that's not me to the channel
	conn.Event(&messageEvent{
		ID:          "user-2",
		Channel:     "some-dm",
		ChannelType: channelTypeChannel,
		Thread:      "",         // channel mode
		User:        "not-phil", // not phil!
		Message:     "echo i am not phil",
	})
	conn.Event(&messageEvent{
		ID:          "user-3",
		Channel:     "some-dm",
		ChannelType: channelTypeChannel,
		Thread:      "",     // channel mode
		User:        "phil", // phil
		Message:     "echo i am phil",
	})
	assert.True(t, conn.MessageContainsWait("2", "i am phil"))
	assert.NotContains(t, conn.Message("1").Message, "i am not phil")

	// Add "not-phil" to the allow list
	conn.Event(&messageEvent{
		ID:          "user-4",
		Channel:     "some-dm",
		ChannelType: channelTypeChannel,
		Thread:      "", // channel mode
		User:        "phil",
		Message:     "!allow @not-phil",
	})
	assert.True(t, conn.MessageContainsWait("3", "Okay, I added the user(s) to the allow list."))

	// Now "not-phil" can send commands
	conn.Event(&messageEvent{
		ID:          "user-5",
		Channel:     "some-dm",
		ChannelType: channelTypeChannel,
		Thread:      "",         // channel mode
		User:        "not-phil", // not phil!
		Message:     "echo i'm still not phil",
	})
	assert.True(t, conn.MessageContainsWait("2", "i'm still not phil"))
}

func createConfig(t *testing.T) *config.Config {
	tempDir := t.TempDir()
	for name, script := range testScripts {
		scriptFile := filepath.Join(tempDir, name)
		if err := os.WriteFile(scriptFile, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	conf := config.New("mem")
	conf.RefreshInterval = 30 * time.Millisecond
	conf.ScriptDir = tempDir
	return conf
}
