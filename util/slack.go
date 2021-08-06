package util

import (
	"regexp"
	"strings"
)

var (
	slackLinkWithTextRegex = regexp.MustCompile(`<https?://[^|\s]+\|([^>]+)>`)
	slackRawLinkRegex      = regexp.MustCompile(`<(https?://[^|\s]+)>`)
	slackCodeRegex         = regexp.MustCompile("`([^`]+)`")
	slackCodeBlockRegex    = regexp.MustCompile("```([^`]+)```")
	slackUserLinkRegex     = regexp.MustCompile(`<@U[^>]+>`)
	slackReplacer          = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">") // see slackutilsx.go, EscapeMessage
)

// RemoveSlackMarkup removes Slack's awkward markup language, i.e. links (<http://..>, <http://..|text>), code (`..`, ```...```),
// user links (<@U...>), and unescapes the few characters that Slack escapes.
func RemoveSlackMarkup(text string) string {
	text = slackLinkWithTextRegex.ReplaceAllString(text, "$1")
	text = slackRawLinkRegex.ReplaceAllString(text, "$1")
	text = slackCodeRegex.ReplaceAllString(text, "$1")
	text = slackCodeBlockRegex.ReplaceAllString(text, "$1")
	text = slackUserLinkRegex.ReplaceAllString(text, "") // Remove entirely!
	text = slackReplacer.Replace(text)                   // Must happen last!
	return text
}
