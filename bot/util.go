package bot

import (
	"archive/zip"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strings"
	"unicode"
)

var (
	// consoleCodeRegex is a regex describing console escape sequences that we're stripping out. This regex
	// only matches ECMA-48 CSI sequences (ESC [ ... <char>), which is enough since, we're using tmux's capture-pane.
	// See https://man7.org/linux/man-pages/man4/console_codes.4.html
	consoleCodeRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

	unquoteReplacer = strings.NewReplacer(
		"\\n", "\n", // new line
		"\\r", "\r", // line feed
		"\\t", "\t", // tab
		"\\b", "\b", // backspace
	)
	unquoteHexCharRegex = regexp.MustCompile(`\\x[a-fA-F0-9]{2}`)

	tmuxWindowRegex = regexp.MustCompile(`│·*$|─+$|─*┘·*$|·+$|·*\(size \d+x\d+ from a smaller client\)\s*$`)
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
	// FIXME this is still wrong i think; also we need to expand empty lines at the beginning
	if strings.TrimSpace(lines[len(lines)-1]) != "" || strings.TrimSpace(lines[len(lines)-2]) != "" {
		return window
	}
	lines[len(lines)-1] = ".\n"
	return strings.Join(lines, "\n")
}

func cropWindow(window string, limit int) string {
	if len(window) < limit {
		return window
	}
	lines := strings.Split(window, "\n")
	if len(lines) <= 2 {
		return window[:limit-1]
	}
	cropMessage := "   (Cropped due to platform limit)   "
	if len(lines[1]) < len(cropMessage) {
		lines[1] = cropMessage
	} else {
		lines[1] = cropMessage + lines[1][len(cropMessage):]
	}
	maxlen := int(math.Ceil(float64(limit)/float64(len(lines)))) - 1
	for i := range lines {
		if len(lines[i]) > maxlen {
			lines[i] = lines[i][:maxlen]
		}
	}
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

func removeTmuxBorder(window string) string {
	lines := strings.Split(window, "\n")
	for i := range lines {
		lines[i] = strings.TrimRightFunc(tmuxWindowRegex.ReplaceAllString(lines[i], ""), unicode.IsSpace)
	}
	return strings.Join(lines, "\n")
}

func zipAppendFile(zw *zip.Writer, name string, filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

func zipAppendEntry(zw *zip.Writer, name string, content string) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(content)); err != nil {
		return err
	}
	return nil
}
