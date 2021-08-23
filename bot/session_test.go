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
	assert.True(t, conn.MessageContainsWait("1", "REPL session started, @phil"))
	assert.True(t, conn.MessageContainsWait("2", "Enter name:"))

	sess.UserInput("phil", "Phil")
	assert.True(t, conn.MessageContainsWait("2", "Hello Phil!"))

	sess.UserInput("phil", "!help")
	assert.True(t, conn.MessageContainsWait("3", "Send empty return"))

	sess.UserInput("phil", "!c")
	assert.True(t, util.WaitUntil(func() bool { return !sess.Active() }, time.Second))
}

func TestBashShell(t *testing.T) {
	sess, conn := createSession(t, testBashREPL)
	defer sess.ForceClose()

	dir := t.TempDir()
	sess.UserInput("phil", "cd "+dir)
	sess.UserInput("phil", "vi hi.txt")
	assert.True(t, conn.MessageContainsWait("2", "~\n~\n~\n~"))

	sess.UserInput("phil", "!n i")
	assert.True(t, conn.MessageContainsWait("2", "-- INSERT --"))

	sess.UserInput("phil", "!n I'm writing this in vim.")
	sess.UserInput("phil", "!esc")
	sess.UserInput("phil", "!n yyp")
	sess.UserInput("phil", ":wq\n")
	assert.True(t, util.WaitUntil(func() bool {
		b, _ := os.ReadFile(filepath.Join(dir, "hi.txt"))
		return b != nil && string(b) == "I'm writing this in vim.\nI'm writing this in vim.\n"
	}, time.Second))

	sess.UserInput("phil", "!q")
	assert.True(t, util.WaitUntil(func() bool { return !sess.Active() }, time.Second))
}

func createSession(t *testing.T, script string) (*session, *memConn) {
	tempDir := t.TempDir()
	scriptFile := filepath.Join(tempDir, "repl.sh")
	if err := os.WriteFile(scriptFile, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	conf := config.New("")
	conf.RefreshInterval = 30 * time.Millisecond
	conn := newMemConn(conf)
	sconfig := &sessionConfig{
		ID:          "sess_" + util.RandomString(5),
		User:        "phil",
		Control:     &chatID{"channel", "thread"},
		Terminal:    &chatID{"channel", ""},
		Script:      scriptFile,
		ControlMode: config.Split,
		WindowMode:  config.Full,
		AuthMode:    config.Everyone,
		Size:        config.Small,
		RelayPort:   0,
	}
	sess := newSession(conf, conn, sconfig)
	go sess.Run()
	return sess, conn
}
