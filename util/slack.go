package util

import (
	"regexp"
	"strings"
)

var (
	slackLinkWithTextRegex = regexp.MustCompile(`<https?://[^|\s]+\|([^>]+)>`)
	slackRawLinkRegex      = regexp.MustCompile(`<(https?://[^|\s]+)>`)
	slackCodeRegex         = regexp.MustCompile("`([^`]+)`")
	slackUserLinkRegex     = regexp.MustCompile(`<@U[^>]+>`)
	slackReplacer          = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">") // see slackutilsx.go, EscapeMessage
)

func RemoveSlackMarkup(text string) string {
	text = slackLinkWithTextRegex.ReplaceAllString(text, "$1")
	text = slackRawLinkRegex.ReplaceAllString(text, "$1")
	text = slackCodeRegex.ReplaceAllString(text, "$1")
	text = slackUserLinkRegex.ReplaceAllString(text, "") // Remove entirely!
	text = slackReplacer.Replace(text)
	return text
}
