package bridge

// User story: «Включи музыку с сайта abc.su/music» has no word rule any more,
// so a classifier decides it. When it answers llm, the phrase must start the
// site music directly, exactly as a matching llm rule does, instead of going
// to Claude. A phrase the music handler does not recognise stays with Claude,
// and a yandex or home answer is never turned into music.

import (
	"context"
	"testing"

	"quasarbridge/internal/llm"
)

type musicProbe struct{ can bool }

func (m musicProbe) CanStartMusic(string) bool { return m.can }
func (musicProbe) StartMusic(context.Context, string) (string, error) {
	return "Включаю музыку.", nil
}

func classifierDecision(t *testing.T, backend, route string, can bool) RouteDecision {
	t.Helper()
	claude := func(context.Context, []llm.Message, bool) (string, error) {
		return `{"route":"` + route + `"}`, nil
	}
	chooser := func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		if backend == "decisions" {
			return llm.RouteAnswer{Route: route}, nil
		}
		return llm.RouteAnswer{}, &llm.DecisionsError{Class: "timeout"}
	}
	e := decisionsEngine(t, DefaultRoutingConfig(), chooser, claude)
	e.Music = musicProbe{can}
	e.Home = homeFunc(func(context.Context, string) (string, error) { return "", nil })
	return e.Decide(t.Context(), "Включи музыку с сайта abc.su/music")
}

func TestClassifierLLMAnswerStartsSiteMusic(t *testing.T) {
	for _, backend := range []string{"decisions", "llm"} {
		t.Run(backend, func(t *testing.T) {
			d := classifierDecision(t, backend, "llm", true)
			if d.Route != "music" || d.Source != "classifier" || d.Backend != backend {
				t.Fatalf("decision=%+v", d)
			}
		})
	}
}

func TestClassifierLLMAnswerStaysWithClaudeWhenItIsNotAMusicCommand(t *testing.T) {
	for _, backend := range []string{"decisions", "llm"} {
		t.Run(backend, func(t *testing.T) {
			if d := classifierDecision(t, backend, "llm", false); d.Route != "llm" {
				t.Fatalf("decision=%+v", d)
			}
		})
	}
}

func TestClassifierOtherAnswersAreNeverTurnedIntoMusic(t *testing.T) {
	for _, route := range []string{"yandex", "home"} {
		for _, backend := range []string{"decisions", "llm"} {
			t.Run(route+"/"+backend, func(t *testing.T) {
				if d := classifierDecision(t, backend, route, true); d.Route != route {
					t.Fatalf("decision=%+v", d)
				}
			})
		}
	}
}
