package bot

import (
	"fmt"
	"heckel.io/replbot/config"
	"strconv"
	"strings"
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

func expandWindow(window string) string {
	lines := strings.Split(window, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[len(lines)-1]) != "" {
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
