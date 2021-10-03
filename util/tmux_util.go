package util

import (
	_ "embed" // required by go:embed
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

const (
	minimumTmuxVersion    = 2.6 // see issue #39
	windowSizeTmuxVersion = 2.9 // see issue #44
)

var (
	tmuxVersionRegex = regexp.MustCompile(`tmux (\d+\.\d+)`)
)

// CheckTmuxVersion checks the version of tmux and returns an error if it's not supported
func CheckTmuxVersion() error {
	return checkTmuxVersion(minimumTmuxVersion)
}

// supportsTmuxWindowSize checks if the "window-size" option is supported (tmux >= 2.9)
func supportsTmuxWindowSize() bool {
	return checkTmuxVersion(windowSizeTmuxVersion) == nil
}

func checkTmuxVersion(compareVersion float64) error {
	cmd := exec.Command("tmux", "-V")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	matches := tmuxVersionRegex.FindStringSubmatch(string(output))
	if len(matches) <= 1 {
		return errors.New("unexpected tmux version output")
	}
	version, err := strconv.ParseFloat(matches[1], 32)
	if err != nil {
		return err
	}
	if version < compareVersion-0.01 { // floats are fun
		return fmt.Errorf("tmux version too low: tmux %.1f required, but found tmux %.1f", compareVersion, version)
	}
	return nil
}
