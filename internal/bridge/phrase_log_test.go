package bridge

// User story (owner, 2026-11-25): «включи группу Кино» went to the smart
// home three times, and the log could not show what the speaker heard. With
// phrase logging on (QUASAR_LOG_PHRASES=1 on Pi 3), each recognized phrase is
// logged with its request tag next to the route decision, so a wrong route
// is explained by the words, not guessed. Off, the log stays without the
// spoken words, as upstream keeps it.

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func capturePhraseLog(t *testing.T, n *Native, text string) string {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	n.logPhrase("abcd1234", text)
	return buf.String()
}

func TestRecognizedPhraseIsLoggedWhenPhraseLoggingIsOn(t *testing.T) {
	out := capturePhraseLog(t, &Native{LogPhrases: true}, "включи группу кино")
	var line map[string]any
	if err := json.Unmarshal([]byte(out), &line); err != nil {
		t.Fatalf("log %q: %v", out, err)
	}
	if line["msg"] != "native_phrase" || line["request"] != "abcd1234" || line["text"] != "включи группу кино" {
		t.Fatalf("line %v", line)
	}
}

func TestRecognizedPhraseStaysOutOfTheLogByDefault(t *testing.T) {
	if out := capturePhraseLog(t, &Native{}, "включи группу кино"); strings.Contains(out, "кино") {
		t.Fatalf("phrase logged with logging off: %q", out)
	}
}
