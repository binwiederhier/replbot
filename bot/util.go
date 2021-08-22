package bot

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

var (
	unquoteReplacer = strings.NewReplacer(
		"\\n", "\n", // new line
		"\\r", "\r", // line feed
		"\\t", "\t", // tab
		"\\b", "\b", // backspace
	)
	unquoteHexCharRegex = regexp.MustCompile(`\\x[a-fA-F0-9]{2}`)
)

func addCursor(window string, x, y int) string {
	lines := strings.Split(window, "\n")
	if len(lines) <= y {
		return window
	}
	line := lines[y]
	if len(line) < x+1 {
		line += strings.Repeat(" ", x-len(line)+1)
	}
	lines[y] = line[0:x] + "█" + line[x+1:]
	return strings.Join(lines, "\n")
}

func addExitedMessage(window string) string {
	lines := strings.Split(window, "\n")
	if len(lines) <= 2 {
		return window
	}
	if strings.TrimSpace(lines[len(lines)-1]) == "" && (strings.TrimSpace(lines[len(lines)-2]) == "" || strings.TrimSpace(lines[len(lines)-2]) == ".") {
		lines[len(lines)-2] = "(REPL exited.)"
		return strings.Join(lines, "\n")
	}
	return window + "\n(REPL exited.)\n"
}

func expandWindow(window string) string {
	lines := strings.Split(window, "\n")
	if len(lines) <= 2 {
		return window
	}
	if strings.TrimSpace(lines[len(lines)-1]) != "" || strings.TrimSpace(lines[len(lines)-2]) != "" {
		return window
	}
	lines[len(lines)-1] = ".\n"
	return strings.Join(lines, "\n")
}

func unquote(s string) string {
	s = unquoteReplacer.Replace(s)
	s = unquoteHexCharRegex.ReplaceAllStringFunc(s, func(r string) string {
		b, _ := hex.DecodeString(r[2:]) // strip \x prefix
		return string(b)
	})
	return s
}

func sanitizeWindow(window string) string {
	sanitized := consoleCodeRegex.ReplaceAllString(window, "")
	if strings.TrimSpace(sanitized) == "" {
		sanitized = fmt.Sprintf("(screen is empty) %s", sanitized)
	}
	return sanitized
}
