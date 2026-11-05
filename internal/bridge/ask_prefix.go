package bridge

import (
	"regexp"
	"strings"
)

var askModelPrefix = regexp.MustCompile(`(?i)^\s*(?:алиса[,\s]+)?спроси\s+нейросеть(?:[,\s:;.!?—–-]+|$)`)

// stripAskModelPrefix leaves the bare question for the model. Fork: a phrase
// with nothing after the prefix stays as it is.
func stripAskModelPrefix(text string) string {
	span := askModelPrefix.FindStringIndex(text)
	if span == nil {
		return text
	}
	if question := strings.TrimSpace(text[span[1]:]); question != "" {
		return question
	}
	return text
}
