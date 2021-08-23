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
  run) bash -i ;;
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
	assert.True(t, util.WaitUntilNot(sess.Active, time.Second))
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
	assert.True(t, util.WaitUntilNot(sess.Active, time.Second))
}

func TestSessionCommands(t *testing.T) {
	sess, conn := createSession(t, testBashREPL)
	defer sess.ForceClose()

	dir := t.TempDir()
	sess.UserInput("phil", "!e echo \"Phil\\bL\\nwas here\\ris here\"\\n")
	assert.True(t, conn.MessageContainsWait("2", "PhiL\n> was here\n> is here")) // The terminal converts \r to \n, whoa!

	sess.UserInput("phil", "!n echo this")
	sess.UserInput("phil", "is it")
	assert.True(t, conn.MessageContainsWait("2", "thisis it"))

	sess.UserInput("phil", "!s")
	sess.UserInput("phil", "echo hi there")
	assert.True(t, conn.MessageContainsWait("3", "hi there"))

	sess.UserInput("phil", "cd "+dir)
	sess.UserInput("phil", "touch test-b test-a test-c test-d")
	sess.UserInput("phil", "!n ls test-")
	sess.UserInput("phil", "!tt")
	assert.True(t, conn.MessageContainsWait("3", "test-a  test-b  test-c  test-d"))

	sess.UserInput("phil", "!c")
	sess.UserInput("phil", "!d")
	assert.True(t, util.WaitUntilNot(sess.Active, time.Second))
}

func TestSessionResize(t *testing.T) {
	// FIXME stty size reports 39 99, why??

	/*
		sess, conn := createSession(t, testBashREPL)
		defer sess.ForceClose()

		sess.UserInput("phil", "stty size")
		assert.True(t, conn.MessageContainsWait("3", "24 80"))
		conn.LogMessages()

		time.Sleep(time.Second)
		sess.UserInput("phil", "!resize large")
		sess.UserInput("phil", "stty size")
		assert.True(t, conn.MessageContainsWait("3", "100 30"))

		sess.UserInput("phil", "!d")
		assert.True(t, util.WaitUntilNot(sess.Active, time.Second))
	*/
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
