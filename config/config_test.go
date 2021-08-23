package config

import (
	"github.com/stretchr/testify/assert"
	"os"
	"path/filepath"
	"testing"
)

func TestNew(t *testing.T) {
	dir := t.TempDir()
	script1 := filepath.Join(dir, "script1")
	script2 := filepath.Join(dir, "script2")
	if err := os.WriteFile(script1, []byte{}, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script2, []byte{}, 0700); err != nil {
		t.Fatal(err)
	}
	conf := New("xoxb-slack-token")
	conf.ScriptDir = dir
	assert.Equal(t, Slack, conf.Platform())
	assert.False(t, conf.ShareEnabled())
	assert.Empty(t, conf.Script("does-not-exist"))
	assert.Equal(t, script1, conf.Script("script1"))
	assert.Equal(t, script2, conf.Script("script2"))
}

func TestNewDiscordShareHost(t *testing.T) {
	conf := New("not-slack")
	conf.ShareHost = "localhost:2586"
	assert.Equal(t, Discord, conf.Platform())
	assert.True(t, conf.ShareEnabled())
}
