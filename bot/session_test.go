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

func TestSessionCustomShell(t *testing.T) {
	sess, conn := createSession(t, "enter-prefix")
	defer sess.ForceClose()
	assert.True(t, conn.MessageContainsWait("1", "REPL session started, @phil"))
	assert.True(t, conn.MessageContainsWait("2", "Enter prefix:"))

	sess.UserInput("phil", "Phil")
	assert.True(t, conn.MessageContainsWait("2", "Hello Phil!"))

	sess.UserInput("phil", "!help")
	assert.True(t, conn.MessageContainsWait("3", "Send empty return"))

	sess.UserInput("phil", "!c")
	assert.True(t, util.WaitUntilNot(sess.Active, maxWaitTime))
}

func TestBashShell(t *testing.T) {
	sess, conn := createSession(t, "bash")
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
	assert.True(t, util.WaitUntilNot(sess.Active, maxWaitTime))
}

func TestSessionCommands(t *testing.T) {
	sess, conn := createSession(t, "bash")
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
	assert.True(t, util.WaitUntilNot(sess.Active, maxWaitTime))
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
	conf := createConfig(t)
	conn := newMemConn(conf)
	sconfig := &sessionConfig{
		global:      conf,
		id:          "sess_" + util.RandomString(5),
		user:        "phil",
		control:     &channelID{"channel", "thread"},
		terminal:    &channelID{"channel", ""},
		script:      conf.Script(script),
		controlMode: config.Split,
		windowMode:  config.Full,
		authMode:    config.Everyone,
		size:        config.Small,
	}
	sess := newSession(sconfig, conn)
	go sess.Run()
	return sess, conn
}
