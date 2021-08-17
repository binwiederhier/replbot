package util

import (
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
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

func StringContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

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
