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
	session, conn := createSession(t, testREPL)
	defer session.ForceClose()

	time.Sleep(200 * time.Millisecond)
	assert.Contains(t, conn.Message("1").Message, "REPL session started, @phil")
	assert.Contains(t, conn.Message("2").Message, `Enter name:`)

	session.UserInput("phil", "Phil")
	time.Sleep(100 * time.Millisecond)
	assert.Contains(t, conn.Message("2").Message, `Hello Phil!`)

	session.UserInput("phil", "!help")
	time.Sleep(100 * time.Millisecond)
	assert.Contains(t, conn.Message("3").Message, `Send empty return`)

	session.UserInput("phil", "!c")
	time.Sleep(100 * time.Millisecond)
	assert.False(t, session.Active())
}

func TestBashShell(t *testing.T) {
	session, conn := createSession(t, testBashREPL)
	defer session.ForceClose()
	time.Sleep(100 * time.Millisecond)

	dir := t.TempDir()
	session.UserInput("phil", "cd "+dir)
	session.UserInput("phil", "vi hi.txt")
	time.Sleep(100 * time.Millisecond)
	assert.Contains(t, conn.Message("2").Message, "~\n~\n~\n~")

	session.UserInput("phil", "!n i")
	time.Sleep(100 * time.Millisecond)
	assert.Contains(t, conn.Message("2").Message, "-- INSERT --")

	session.UserInput("phil", "!n I'm writing this in vim.")
	session.UserInput("phil", "!esc")
	session.UserInput("phil", "!n yyp")
	session.UserInput("phil", ":wq\n")
	time.Sleep(100 * time.Millisecond)
	b, _ := os.ReadFile(filepath.Join(dir, "hi.txt"))
	assert.Equal(t, "I'm writing this in vim.\nI'm writing this in vim.\n", string(b))

	session.UserInput("phil", "!q")
	time.Sleep(100 * time.Millisecond)
	assert.False(t, session.Active())
}

func createSession(t *testing.T, script string) (*Session, *MemConn) {
	tempDir := t.TempDir()
	scriptFile := filepath.Join(tempDir, "repl.sh")
	if err := os.WriteFile(scriptFile, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	conf := config.New("")
	conf.RefreshInterval = 50 * time.Millisecond
	conn := NewMemConn(conf)
	sconfig := &SessionConfig{
		ID:          "sess_" + util.RandomString(5),
		User:        "phil",
		Control:     &ChatID{"main", ""},
		Terminal:    &ChatID{"main", ""},
		Script:      scriptFile,
		ControlMode: config.Channel,
		WindowMode:  config.Full,
		AuthMode:    config.OnlyMe,
		Size:        config.Small,
		RelayPort:   0,
	}
	session := NewSession(conf, conn, sconfig)
	go session.Run()
	return session, conn
}
