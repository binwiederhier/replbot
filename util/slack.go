package util

import (
	"regexp"
	"strings"
)

var (
	slackLinkWithTextRegex = regexp.MustCompile(`<https?://[^|\s]+\|([^>]+)>`)
	slackRawLinkRegex      = regexp.MustCompile(`<(https?://[^|\s]+)>`)
	slackCodeBlockRegex    = regexp.MustCompile("```([^`]+)```")
	slackCodeRegex         = regexp.MustCompile("`([^`]+)`")
	slackUserLinkRegex     = regexp.MustCompile(`<@U[^>]+>`)
	slackMacQuotesRegex    = regexp.MustCompile(`[“”]`)
	slackReplacer          = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">") // see slackutilsx.go, EscapeMessage
)

// RemoveSlackMarkup removes Slack's awkward markup language, i.e. links (<http://..>, <http://..|text>), code (`..`, ```...```),
// user links (<@U...>), and unescapes the few characters that Slack escapes.
func RemoveSlackMarkup(text string) string {
	text = slackLinkWithTextRegex.ReplaceAllString(text, "$1")
	text = slackRawLinkRegex.ReplaceAllString(text, "$1")
	text = slackCodeBlockRegex.ReplaceAllString(text, "$1")
	text = slackCodeRegex.ReplaceAllString(text, "$1")
	text = slackUserLinkRegex.ReplaceAllString(text, "")   // Remove entirely!
	text = slackMacQuotesRegex.ReplaceAllString(text, `"`) // See issue #14, Mac client sends wrong quotes
	text = slackReplacer.Replace(text)                     // Must happen last!
	return text
}
