package util

import (
	"os"
	"os/exec"
	"regexp"
)

var (
	nonAlphanumericCharsRegex = regexp.MustCompile(`[^A-Za-z0-9]`)
)

func SanitizeID(id string) string {
	return nonAlphanumericCharsRegex.ReplaceAllString(id, "_")
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
