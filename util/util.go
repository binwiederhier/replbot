package util

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
)

var (
	invalidTmuxIDCharsRegex = regexp.MustCompile(`[^A-Za-z0-9]`)
)

func SanitizeID(id string) string {
	id = fmt.Sprintf("replbot_%s", id)
	return invalidTmuxIDCharsRegex.ReplaceAllString(id, "_")
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
