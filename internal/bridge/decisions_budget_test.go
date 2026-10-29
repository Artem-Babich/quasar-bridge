package bridge

// User story: when Decisions times out, Claude still gets the rest of
// timeout_ms to classify the phrase, so a slow OpenAI does not push requests
// to on_error. Decisions answers are logged with their confidence even when
// it is 0, and the decision says how long the classifier step took.

import (
	"context"
	"errors"
	"testing"
	"time"

	"quasarbridge/internal/llm"
)

func TestDecisionsTimeoutLeavesClaudeTheRestOfTimeoutMs(t *testing.T) {
	cfg := DefaultRoutingConfig()
	cfg.Classifier.TimeoutMS = 1000
	cfg.Classifier.DecisionsTimeoutMS = 100
	var claudeBudget time.Duration
	start := time.Now()
	e := decisionsEngine(t, cfg, func(ctx context.Context, _ string, _ []llm.Message, _ []string) (llm.RouteAnswer, error) {
		<-ctx.Done()
		return llm.RouteAnswer{}, &llm.DecisionsError{Class: "timeout"}
	}, func(ctx context.Context, _ []llm.Message, _ bool) (string, error) {
		deadline, _ := ctx.Deadline()
		claudeBudget = deadline.Sub(start)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return `{"route":"llm"}`, nil
	})
	d := e.Decide(t.Context(), "сочини стих про кота")
	if d.Route != "llm" || d.Source != "classifier" || d.Backend != "llm" || d.ErrorClass != "timeout" {
		t.Fatalf("decision=%+v", d)
	}
	if claudeBudget < 900*time.Millisecond || claudeBudget > 1010*time.Millisecond {
		t.Fatalf("Claude deadline %v after start, want about timeout_ms 1000", claudeBudget)
	}
}

func TestBothClassifiersFailingStillReportsLlmBackend(t *testing.T) {
	e := decisionsEngine(t, DefaultRoutingConfig(), func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{}, &llm.DecisionsError{Class: "refusal"}
	}, func(context.Context, []llm.Message, bool) (string, error) { return "", errors.New("offline") })
	if d := e.Decide(t.Context(), "сочини стих про кота"); d.Source != "classifier_error" || d.Backend != "llm" {
		t.Fatalf("decision=%+v", d)
	}
}

func TestZeroConfidenceFromDecisionsIsLogged(t *testing.T) {
	e := decisionsEngine(t, DefaultRoutingConfig(), func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{Route: "llm", Confidence: 0}, nil
	}, failingClaude(t))
	line := logLine(t, routeDecisionLogArgs("abcd1234", e.Decide(t.Context(), "сочини стих")))
	if v, ok := line["confidence"]; !ok || v != float64(0) {
		t.Fatalf("confidence missing: %v", line)
	}
}

func TestDecisionsAnswerReportsClassifierLatency(t *testing.T) {
	e := decisionsEngine(t, DefaultRoutingConfig(), func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		time.Sleep(20 * time.Millisecond)
		return llm.RouteAnswer{Route: "llm", Confidence: 0.6}, nil
	}, failingClaude(t))
	if d := e.Decide(t.Context(), "сочини стих"); d.Backend != "decisions" || d.LatencyMS < 20 {
		t.Fatalf("decision=%+v", d)
	}
}
