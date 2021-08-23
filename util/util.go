package util

import (
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	nonAlphanumericCharsRegex = regexp.MustCompile(`[^A-Za-z0-9]`)
	random                    = rand.New(rand.NewSource(time.Now().UnixNano()))
	randomStringCharset       = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// SanitizeNonAlphanumeric replaces all non-alphanumeric characters with an underscore
func SanitizeNonAlphanumeric(s string) string {
	return nonAlphanumericCharsRegex.ReplaceAllString(s, "_")
}

// FileExists returns true if a file with the given filename exists
func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

// Run is a shortcut running an exec.Command
func Run(command ...string) error {
	cmd := exec.Command(command[0], command[1:]...)
	return cmd.Run()
}

// RunAll runs all the given commands
func RunAll(commands ...[]string) error {
	for _, command := range commands {
		if err := Run(command...); err != nil {
			return err
		}
	}
	return nil
}

// RandomPort finds a free random, operating system chosen port
func RandomPort() (int, error) {
	listener, err := net.Listen("tcp4", ":0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	_, p, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return 0, err
	}
	return port, nil
}

// RandomString returns a random alphanumeric string of the given length
func RandomString(length int) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = randomStringCharset[random.Intn(len(randomStringCharset))]
	}
	return string(b)
}

// FormatMarkdownCode formats the given string as a markdown code block
func FormatMarkdownCode(s string) string {
	return fmt.Sprintf("```%s```", strings.ReplaceAll(s, "```", "` ` `")) // Hack ...
}

// StringContainsWait is a helper function for tests to check if a string is contained within a haystack
// within a reasonable amount of time
func StringContainsWait(haystackFn func() string, needle string, maxWait time.Duration) (contains bool) {
	fn := func() bool {
		return strings.Contains(haystackFn(), needle)
	}
	return WaitUntil(fn, maxWait)
}

// WaitUntil waits for fn to turn true within a reasonable amount of time
func WaitUntil(fn func() bool, maxWait time.Duration) bool {
	for start := time.Now(); time.Since(start) < maxWait; {
		if fn() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
