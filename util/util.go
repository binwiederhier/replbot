package util

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"regexp"
	"time"
)

var (
	random                  = rand.New(rand.NewSource(time.Now().UnixNano()))
	charsetRandomID         = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	invalidTmuxIDCharsRegex = regexp.MustCompile(`[^A-Za-z0-9]`)
)

func SanitizeID(id string) string {
	id = fmt.Sprintf("replbot_%s", id)
	return invalidTmuxIDCharsRegex.ReplaceAllString(id, "_")
}

// RandomStringWithCharset returns a random string with a given length, using the defined charset
func RandomStringWithCharset(length int, charset string) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[random.Intn(len(charset))]
	}
	return string(b)
}

// FileExists returns true if a file with the given filename exists
func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

func Run(command ...string) error {
	cmd := exec.Command(command[0], command[1:]...)
	return cmd.Run()
}

func RunAll(commands ...[]string) error {
	for _, command := range commands {
		if err := Run(command...); err != nil {
			return err
		}
	}
	return nil
}
