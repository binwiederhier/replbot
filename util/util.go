// Package util is a general utility package for REPLbot
package util

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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

// SSHKeyPair represents an SSH key pair
type SSHKeyPair struct {
	PrivateKey string
	PublicKey  string
}

// SanitizeNonAlphanumeric replaces all non-alphanumeric characters with an underscore
func SanitizeNonAlphanumeric(s string) string {
	return nonAlphanumericCharsRegex.ReplaceAllString(s, "_")
}

// FileExists returns true if a file with the given filename exists
func FileExists(filenames ...string) bool {
	for _, filename := range filenames {
		if _, err := os.Stat(filename); err != nil {
			return false
		}
	}
	return true
}

// Run is a shortcut running an exec.Command
func Run(command ...string) error {
	cmd := exec.Command(command[0], command[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command failed: %s\ncommand: %s\ncommand output: %s", err.Error(), strings.Join(command, " "), string(output))
	}
	return nil
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

// InStringList returns true if needle is contained in the list of strings
func InStringList(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
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

// WaitUntilNot waits for fn to turn false within a reasonable amount of time
func WaitUntilNot(fn func() bool, maxWait time.Duration) bool {
	return WaitUntil(func() bool { return !fn() }, maxWait)
}

// TempFileName generates a random file name for a file in the temp folder
func TempFileName() string {
	return filepath.Join(os.TempDir(), "replbot_"+RandomString(10))
}

// GenerateSSHKeyPair generates an SSH key pair
func GenerateSSHKeyPair() (pair *SSHKeyPair, err error) {
	privKeyFile := TempFileName()
	pubKeyFile := privKeyFile + ".pub"
	defer func() {
		os.Remove(privKeyFile)
		os.Remove(pubKeyFile)
	}()
	if err := Run("ssh-keygen", "-t", "rsa", "-f", privKeyFile, "-q", "-N", ""); err != nil {
		return nil, err
	}
	privKey, err := os.ReadFile(privKeyFile)
	if err != nil {
		return nil, err
	}
	pubKey, err := os.ReadFile(pubKeyFile)
	if err != nil {
		return nil, err
	}
	pubKeyFields := strings.Fields(string(pubKey))
	if len(pubKeyFields) != 3 {
		return nil, errors.New("unexpected public key format")
	}
	return &SSHKeyPair{string(privKey), strings.Join(pubKeyFields[0:2], " ")}, nil
}
