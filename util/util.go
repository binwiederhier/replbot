package util

import (
	"errors"
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

func TCPPort() (int, error) {
	for i := 0; i < 20; i++ {
		listener, err := net.Listen("tcp4", ":0")
		if err != nil {
			continue
		}
		_, p, err := net.SplitHostPort(listener.Addr().String())
		if err != nil {
			continue
		}
		port, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		_ = listener.Close()
		return port, nil
	}
	return 0, errors.New("unable to find free port")
}
