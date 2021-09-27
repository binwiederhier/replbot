package bot

import (
	"archive/zip"
	"fmt"
	"github.com/stretchr/testify/assert"
	"heckel.io/replbot/config"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

func TestBotBashWebTerminal(t *testing.T) {
	conf := createConfig(t)
	conf.WebHost = "localhost:12123"
	conf.DefaultWeb = true
	robot, err := New(conf)
	if err != nil {
		t.Fatal(err)
	}
	go robot.Run()
	defer robot.Stop()
	conn := robot.conn.(*memConn)

	// Start in channel mode with web terminal
	conn.Event(&messageEvent{
		ID:          "user-1",
		Channel:     "some-channel",
		ChannelType: channelTypeChannel,
		Thread:      "",
		User:        "phil",
		Message:     "@replbot bash", // 'web' is not mentioned, it's set by default
	})
	assert.True(t, conn.MessageContainsWait("1", "REPL session started, @phil"))
	assert.True(t, conn.MessageContainsWait("1", "Everyone can also *view and control*"))
	assert.True(t, conn.MessageContainsWait("1", "http://localhost:12123/")) // web terminal URL
	assert.True(t, conn.MessageContainsWait("2", "$"))                       // this is stupid ...

	// Check that web terminal actually returns HTML
	for i := 0; ; i++ {
		urlRegex := regexp.MustCompile(`(http://[/:\w]+)`)
		matches := urlRegex.FindStringSubmatch(conn.Message("1").Message)
		webTerminalURL := matches[1]
		resp, err := http.Get(webTerminalURL)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "<html ") {
			break
		}
		if i == 5 {
			t.Fatal("unexpected response: '<html ' not contained in: " + string(body))
		}
		time.Sleep(time.Second)
	}
}

func TestBotBashRecording(t *testing.T) {
	conf := createConfig(t)
	conf.DefaultRecord = false
	robot, err := New(conf)
	if err != nil {
		t.Fatal(err)
	}
	go robot.Run()
	defer robot.Stop()
	conn := robot.conn.(*memConn)

	// Start in channel mode with 'record'
	conn.Event(&messageEvent{
		ID:          "msg-1",
		Channel:     "some-channel",
		ChannelType: channelTypeChannel,
		Thread:      "",
		User:        "phil",
		Message:     "@replbot bash record",
	})
	assert.True(t, conn.MessageContainsWait("1", "REPL session started, @phil"))
	assert.True(t, conn.MessageContainsWait("2", "$")) // this is stupid ...

	// Send a super hard math problem
	conn.Event(&messageEvent{
		ID:          "msg-2",
		Channel:     "some-channel",
		ChannelType: channelTypeChannel,
		Thread:      "msg-1", // split mode
		User:        "phil",
		Message:     "echo $((2 * 5 * 86))",
	})
	assert.True(t, conn.MessageContainsWait("2", "echo $((2 * 5 * 86))"))
	assert.True(t, conn.MessageContainsWait("2", "860"))

	// Quit session
	conn.Event(&messageEvent{
		ID:          "msg-3",
		Channel:     "some-channel",
		ChannelType: channelTypeChannel,
		Thread:      "msg-1", // split mode
		User:        "phil",
		Message:     "exit",
	})
	assert.True(t, conn.MessageContainsWait("3", "REPL exited. You can find a recording of the session in the file below."))
	assert.NotNil(t, conn.Message("3").File)

	file := conn.Message("3").File
	zipFilename := filepath.Join(t.TempDir(), "recording.zip")
	if err := os.WriteFile(zipFilename, file, 0700); err != nil {
		t.Fatal(err)
	}

	targetDir := t.TempDir()
	if err := unzip(zipFilename, targetDir); err != nil {
		t.Fatal(err)
	}
	readme, _ := os.ReadFile(filepath.Join(targetDir, "REPLbot session", "README.md"))
	terminal, _ := os.ReadFile(filepath.Join(targetDir, "REPLbot session", "terminal.txt"))
	replay, _ := os.ReadFile(filepath.Join(targetDir, "REPLbot session", "replay.asciinema"))
	assert.Contains(t, string(readme), "This ZIP archive contains")
	assert.Contains(t, string(terminal), "echo $((2 * 5 * 86))")
	assert.Contains(t, string(terminal), "860")
	assert.Contains(t, string(replay), "echo $((2 * 5 * 86))")
	assert.Contains(t, string(replay), "860")
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

// unzip extract a zip archive
// from: https://stackoverflow.com/a/24792688/1440785
func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := r.Close(); err != nil {
			panic(err)
		}
	}()

	os.MkdirAll(dest, 0755)

	// Closure to address file descriptors issue with all the deferred .Close() methods
	extractAndWriteFile := func(f *zip.File) error {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer func() {
			if err := rc.Close(); err != nil {
				panic(err)
			}
		}()

		path := filepath.Join(dest, f.Name)

		// Check for ZipSlip (Directory traversal)
		if !strings.HasPrefix(path, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", path)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, 0755)
		} else {
			os.MkdirAll(filepath.Dir(path), 0755)
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
			if err != nil {
				return err
			}
			defer func() {
				if err := f.Close(); err != nil {
					panic(err)
				}
			}()

			_, err = io.Copy(f, rc)
			if err != nil {
				return err
			}
		}
		return nil
	}

	for _, f := range r.File {
		err := extractAndWriteFile(f)
		if err != nil {
			return err
		}
	}

	return nil
}
