package util

import (
	"github.com/stretchr/testify/assert"
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeNonAlphanumeric(t *testing.T) {
	assert.Equal(t, "abc_def_", SanitizeNonAlphanumeric("abc:def?"))
	assert.Equal(t, "_", SanitizeNonAlphanumeric("\U0001F970"))
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "testfile")
	if err := os.WriteFile(file, []byte("hi there"), 0600); err != nil {
		t.Fatal(err)
	}
	assert.True(t, FileExists(file))
	assert.False(t, FileExists("/tmp/not-a-file"))
}

func TestFormatMarkdownCode(t *testing.T) {
	assert.Equal(t, "```this is code```", FormatMarkdownCode("this is code"))
	assert.Equal(t, "```` ` `this is a hack` ` ````", FormatMarkdownCode("```this is a hack```"))
}

func TestRandomPort(t *testing.T) {
	port1, err := RandomPort()
	if err != nil {
		t.Fatal(err)
	}
	port2, err := RandomPort()
	if err != nil {
		t.Fatal(err)
	}
	assert.True(t, port1 > 0 && port1 < 65000)
	assert.True(t, port2 > 0 && port2 < 65000)
	assert.NotEqual(t, port1, port2)
}
