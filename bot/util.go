package bot

import (
	"encoding/hex"
	"fmt"
	"heckel.io/replbot/config"
	"regexp"
	"strconv"
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
	return window + "\n(REPL exited.)"
}

func expandWindow(window string) string {
	lines := strings.Split(window, "\n")
	if len(lines) <= 2 {
		return window
	}
	if strings.TrimSpace(lines[len(lines)-1]) != "" || strings.TrimSpace(lines[len(lines)-2]) != "" {
		return window
	}
	lines[len(lines)-1] = "."
	return strings.Join(lines, "\n")
}

func convertSize(size string) (width int, height int, err error) {
	switch size {
	case config.SizeTiny, config.SizeSmall, config.SizeMedium, config.SizeLarge:
		width, height = config.Sizes[size][0], config.Sizes[size][1]
	default:
		matches := sizeRegex.FindStringSubmatch(size)
		if len(matches) == 0 {
			return 0, 0, fmt.Errorf(malformatedTerminalSizeMessage)
		}
		width, _ = strconv.Atoi(matches[1])
		height, _ = strconv.Atoi(matches[2])
		if width < config.MinSize[0] || height < config.MinSize[1] || width > config.MaxSize[0] || height > config.MaxSize[1] {
			return 0, 0, fmt.Errorf(invalidTerminalSizeMessage, config.MinSize[0], config.MinSize[1], config.MaxSize[0], config.MaxSize[1])
		}
	}
	return
}

func unquote(s string) string {
	s = unquoteReplacer.Replace(s)
	s = unquoteHexCharRegex.ReplaceAllStringFunc(s, func(r string) string {
		b, _ := hex.DecodeString(r[2:]) // strip \x prefix
		return string(b)
	})
	return s
}
