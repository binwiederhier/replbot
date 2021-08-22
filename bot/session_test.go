package bot

import (
	"github.com/stretchr/testify/assert"
	"heckel.io/replbot/config"
	"heckel.io/replbot/util"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testREPL = `
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
`

const testBashREPL = `
#!/bin/bash
case "$1" in
  run) bash -i; ;;
  *) ;;
esac
`

func TestSessionCustomShell(t *testing.T) {
	sess, conn := createSession(t, testREPL)
	defer sess.ForceClose()

	time.Sleep(200 * time.Millisecond)
	assert.Contains(t, conn.Message("1").Message, "REPL session started, @phil")
	assert.Contains(t, conn.Message("2").Message, `Enter name:`)

	sess.UserInput("phil", "Phil")
	time.Sleep(100 * time.Millisecond)
	assert.Contains(t, conn.Message("2").Message, `Hello Phil!`)

	sess.UserInput("phil", "!help")
	time.Sleep(100 * time.Millisecond)
	assert.Contains(t, conn.Message("3").Message, `Send empty return`)

	sess.UserInput("phil", "!c")
	time.Sleep(100 * time.Millisecond)
	assert.False(t, sess.Active())
}

func TestBashShell(t *testing.T) {
	sess, conn := createSession(t, testBashREPL)
	defer sess.ForceClose()
	time.Sleep(100 * time.Millisecond)

	dir := t.TempDir()
	sess.UserInput("phil", "cd "+dir)
	sess.UserInput("phil", "vi hi.txt")
	time.Sleep(100 * time.Millisecond)
	assert.Contains(t, conn.Message("2").Message, "~\n~\n~\n~")

	sess.UserInput("phil", "!n i")
	time.Sleep(100 * time.Millisecond)
	assert.Contains(t, conn.Message("2").Message, "-- INSERT --")

	sess.UserInput("phil", "!n I'm writing this in vim.")
	sess.UserInput("phil", "!esc")
	sess.UserInput("phil", "!n yyp")
	sess.UserInput("phil", ":wq\n")
	time.Sleep(100 * time.Millisecond)
	b, _ := os.ReadFile(filepath.Join(dir, "hi.txt"))
	assert.Equal(t, "I'm writing this in vim.\nI'm writing this in vim.\n", string(b))

	sess.UserInput("phil", "!q")
	time.Sleep(100 * time.Millisecond)
	assert.False(t, sess.Active())
}

func createSession(t *testing.T, script string) (*session, *memConn) {
	tempDir := t.TempDir()
	scriptFile := filepath.Join(tempDir, "repl.sh")
	if err := os.WriteFile(scriptFile, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	conf := config.New("")
	conf.RefreshInterval = 50 * time.Millisecond
	conn := newMemConn(conf)
	sconfig := &sessionConfig{
		ID:          "sess_" + util.RandomString(5),
		User:        "phil",
		Control:     &chatID{"main", ""},
		Terminal:    &chatID{"main", ""},
		Script:      scriptFile,
		ControlMode: config.Channel,
		WindowMode:  config.Full,
		AuthMode:    config.OnlyMe,
		Size:        config.Small,
		RelayPort:   0,
	}
	sess := newSession(conf, conn, sconfig)
	go sess.Run()
	return sess, conn
}
