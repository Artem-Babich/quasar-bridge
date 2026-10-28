package bridge

// User story: when the owner enables the Decisions classifier, a phrase that
// no rule or follow-up catches is routed by Decisions within its own short
// timeout (decisions_timeout_ms, default 1500 ms). If Decisions fails,
// refuses, times out or answers nonsense, Claude decides exactly as before,
// and the log keeps the Decisions failure class. If Claude fails too, the
// configured on_error route is used. The whole classifier step never takes
// longer than timeout_ms. Private dialogue history never leaves for OpenAI.
// Without Decisions the Claude classifier works exactly as before.

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"quasarbridge/internal/dialogue"
	"quasarbridge/internal/llm"
)

type chooserFunc func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error)

func (f chooserFunc) ChooseRoute(ctx context.Context, instructions string, m []llm.Message, routes []string) (llm.RouteAnswer, error) {
	return f(ctx, instructions, m, routes)
}

func decisionsEngine(t *testing.T, cfg RoutingConfig, chooser chooserFunc, claude completionFunc) *Engine {
	t.Helper()
	cfg.Rules = nil
	e := &Engine{LLM: claude, Decisions: chooser}
	if err := e.SetRouting(cfg); err != nil {
		t.Fatal(err)
	}
	return e
}

func failingClaude(t *testing.T) completionFunc {
	return func(context.Context, []llm.Message, bool) (string, error) {
		t.Error("Claude classifier called")
		return "", errors.New("unexpected")
	}
}

func TestDecisionsRouteIsUsedWithoutAskingClaude(t *testing.T) {
	cfg := DefaultRoutingConfig()
	var gotPrompt string
	var gotRoutes []string
	e := decisionsEngine(t, cfg, func(_ context.Context, prompt string, _ []llm.Message, routes []string) (llm.RouteAnswer, error) {
		gotPrompt, gotRoutes = prompt, routes
		return llm.RouteAnswer{Route: "yandex", Confidence: 0.88}, nil
	}, failingClaude(t))
	d := e.Decide(t.Context(), "какой длины Нил")
	if d.Route != "yandex" || d.Source != "classifier" || d.Backend != "decisions" || d.Confidence != 0.88 || d.ErrorClass != "" {
		t.Fatalf("decision=%+v", d)
	}
	if gotPrompt != cfg.Classifier.Prompt || !slices.Equal(gotRoutes, []string{"home", "llm", "yandex"}) {
		t.Fatalf("prompt match=%v routes=%v", gotPrompt == cfg.Classifier.Prompt, gotRoutes)
	}
}

func TestDecisionsFailureFallsBackToClaudeKeepingErrorClass(t *testing.T) {
	cases := map[string]error{
		"http_429":     &llm.DecisionsError{Class: "http", Status: 429},
		"refusal":      &llm.DecisionsError{Class: "refusal"},
		"timeout":      &llm.DecisionsError{Class: "timeout"},
		"bad_response": &llm.DecisionsError{Class: "bad_response"},
		"transport":    errors.New("some other failure"),
	}
	for class, failure := range cases {
		t.Run(class, func(t *testing.T) {
			var claudeCalls atomic.Int32
			e := decisionsEngine(t, DefaultRoutingConfig(), func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
				return llm.RouteAnswer{}, failure
			}, func(context.Context, []llm.Message, bool) (string, error) {
				claudeCalls.Add(1)
				return `{"route":"llm"}`, nil
			})
			d := e.Decide(t.Context(), "сочини стих про кота")
			if d.Route != "llm" || d.Source != "classifier" || d.Backend != "llm" || d.ErrorClass != class || claudeCalls.Load() != 1 {
				t.Fatalf("decision=%+v claude calls=%d", d, claudeCalls.Load())
			}
		})
	}
}

func TestBothClassifiersFailingUsesOnError(t *testing.T) {
	cfg := DefaultRoutingConfig()
	cfg.Classifier.OnError = "llm"
	e := decisionsEngine(t, cfg, func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{}, &llm.DecisionsError{Class: "http", Status: 503}
	}, func(context.Context, []llm.Message, bool) (string, error) {
		return "", errors.New("offline")
	})
	d := e.Decide(t.Context(), "сочини стих про кота")
	if d.Route != "llm" || d.Source != "classifier_error" || d.ErrorClass != "http_503" {
		t.Fatalf("decision=%+v", d)
	}
}

func TestPrivateHistorySendsOnlyCurrentPhrase(t *testing.T) {
	for _, private := range []bool{false, true} {
		t.Run(map[bool]string{false: "shared", true: "private"}[private], func(t *testing.T) {
			var got []llm.Message
			e := decisionsEngine(t, DefaultRoutingConfig(), func(_ context.Context, _ string, m []llm.Message, _ []string) (llm.RouteAnswer, error) {
				got = m
				return llm.RouteAnswer{Route: "llm", Confidence: 0.7}, nil
			}, failingClaude(t))
			e.Dialogue = dialogue.New(dialogue.DefaultConfig())
			_, ticket := e.Dialogue.Begin(t.Context())
			e.Dialogue.Commit(t.Context(), ticket, "найди в истории про пароль", "Нашла запись.", private)
			e.Decide(t.Context(), "сочини стих про кота")
			want := []llm.Message{{Role: "user", Content: "найди в истории про пароль"}, {Role: "assistant", Content: "Нашла запись."}, {Role: "user", Content: "сочини стих про кота"}}
			if private {
				want = want[2:]
			}
			if len(got) != len(want) {
				t.Fatalf("sent %+v, want %+v", got, want)
			}
			for i := range want {
				if got[i].Role != want[i].Role || got[i].Content != want[i].Content {
					t.Fatalf("sent %+v, want %+v", got, want)
				}
			}
		})
	}
}

func TestTotalClassifierTimeNeverExceedsTimeoutMs(t *testing.T) {
	cfg := DefaultRoutingConfig()
	cfg.Classifier.TimeoutMS = 200
	cfg.Classifier.DecisionsTimeoutMS = 150
	var claudeDeadline time.Time
	e := decisionsEngine(t, cfg, func(ctx context.Context, _ string, _ []llm.Message, _ []string) (llm.RouteAnswer, error) {
		<-ctx.Done()
		return llm.RouteAnswer{}, &llm.DecisionsError{Class: "timeout"}
	}, func(ctx context.Context, _ []llm.Message, _ bool) (string, error) {
		claudeDeadline, _ = ctx.Deadline()
		<-ctx.Done()
		return "", ctx.Err()
	})
	start := time.Now()
	d := e.Decide(t.Context(), "сочини стих про кота")
	elapsed := time.Since(start)
	if d.Source != "classifier_error" || d.ErrorClass != "timeout" {
		t.Fatalf("decision=%+v", d)
	}
	if claudeDeadline.After(start.Add(210*time.Millisecond)) || elapsed > 350*time.Millisecond {
		t.Fatalf("classifier step took %v; Claude deadline %v after start", elapsed, claudeDeadline.Sub(start))
	}
}

func TestDecisionsTimeoutZeroMeans1500(t *testing.T) {
	budget := func(timeoutMS, decisionsMS int) time.Duration {
		cfg := DefaultRoutingConfig()
		cfg.Classifier.TimeoutMS = timeoutMS
		cfg.Classifier.DecisionsTimeoutMS = decisionsMS
		var left time.Duration
		e := decisionsEngine(t, cfg, func(ctx context.Context, _ string, _ []llm.Message, _ []string) (llm.RouteAnswer, error) {
			deadline, _ := ctx.Deadline()
			left = time.Until(deadline)
			return llm.RouteAnswer{Route: "llm"}, nil
		}, failingClaude(t))
		e.Decide(t.Context(), "сочини стих про кота")
		return left
	}
	if left := budget(8000, 0); left < 1400*time.Millisecond || left > 1500*time.Millisecond {
		t.Fatalf("default Decisions budget %v, want 1500 ms", left)
	}
	if left := budget(8000, 300); left < 200*time.Millisecond || left > 300*time.Millisecond {
		t.Fatalf("configured Decisions budget %v, want 300 ms", left)
	}
	if left := budget(1000, 0); left < 900*time.Millisecond || left > 1000*time.Millisecond {
		t.Fatalf("Decisions budget %v exceeds timeout_ms 1000", left)
	}
}

func TestDecisionsTimeoutOutsideRangeIsRejected(t *testing.T) {
	for ms, ok := range map[int]bool{-1: false, 0: true, 1: true, 60000: true, 60001: false} {
		cfg := DefaultRoutingConfig()
		cfg.Classifier.DecisionsTimeoutMS = ms
		if _, err := CompileRouting(cfg); (err == nil) != ok {
			t.Fatalf("decisions_timeout_ms=%d accepted=%v", ms, err == nil)
		}
	}
}

func TestWithoutDecisionsClaudePathIsUnchanged(t *testing.T) {
	cfg := DefaultRoutingConfig()
	cfg.Rules = nil
	var got []llm.Message
	var left time.Duration
	e := &Engine{LLM: completionFunc(func(ctx context.Context, m []llm.Message, structured bool) (string, error) {
		got = m
		deadline, _ := ctx.Deadline()
		left = time.Until(deadline)
		return `{"route":"llm"}`, nil
	})}
	if err := e.SetRouting(cfg); err != nil {
		t.Fatal(err)
	}
	d := e.Decide(t.Context(), "сочини стих про кота")
	if d.Route != "llm" || d.Backend != "llm" || d.ErrorClass != "" {
		t.Fatalf("decision=%+v", d)
	}
	if len(got) != 2 || got[0].Role != "system" || got[0].Content != cfg.Classifier.Prompt || got[1].Content != "сочини стих про кота" {
		t.Fatalf("Claude messages changed: %+v", got)
	}
	if left < 7900*time.Millisecond || left > 8000*time.Millisecond {
		t.Fatalf("Claude deadline %v, want timeout_ms 8000", left)
	}
}
