package bridge

// User story: when Claude cannot answer, the speaker says that something went
// wrong. The question is never handed to Alice instead: she would answer
// something else, or act on a phrase meant for Claude. This also holds when
// both classifiers failed first (on_error is llm) and when Claude was the
// fallback after Alice refused.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"quasarbridge/internal/llm"
)

func failingAnswers(context.Context, []llm.Message, bool) (string, error) {
	return "", errors.New("Claude is down")
}

func TestClaudeFailingOnAnAnswerSaysAnErrorAndNeverForwardsToAlice(t *testing.T) {
	cfg := routeContextConfig()
	cfg.Classifier.OnError = "llm"
	e := decisionsEngine(t, cfg, func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{}, &llm.DecisionsError{Class: "http", Status: 503}
	}, failingAnswers)
	r := newSpeakerRig(t, e)
	r.say("Сочини стих")
	f := r.waitUp("TextInput", "TtsGenerate")
	if f.Kind() != "TtsGenerate" {
		t.Fatalf("question was forwarded to Alice: %s", f.Kind())
	}
	if text := f.Client.GetEvent().GetTtsGenerate().GetText(); !strings.Contains(text, "не получилось") {
		t.Fatalf("spoken text %q", text)
	}
}

func TestClaudeFailingAfterAliceRefusedSaysAnErrorInsteadOfHerRefusal(t *testing.T) {
	e := decisionsEngine(t, routeContextConfig(), func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{Route: "yandex"}, nil
	}, failingAnswers)
	r := newSpeakerRig(t, e)
	id := r.say("Сложный вопрос")
	r.waitUp("TextInput")
	r.yandexAnswers(id, "Я не могу помочь с этим.")
	f := r.waitUp("TtsGenerate")
	if text := f.Client.GetEvent().GetTtsGenerate().GetText(); !strings.Contains(text, "не получилось") {
		t.Fatalf("spoken text %q", text)
	}
	for len(r.down) > 0 {
		f := <-r.down
		if text := f.Server.GetEvent().GetAliceResponse().GetVoiceResponse().GetOutputSpeech().GetText(); strings.Contains(text, "не могу помочь") {
			t.Fatal("Alice's refusal reached the speaker")
		}
	}
}
