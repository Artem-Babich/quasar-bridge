package bridge

// User story: the route decision log carries the Decisions call details
// (duration, connection reuse, and on failure OpenAI's error code and request
// ID) so the owner can explain slow or failed classifications from the log.

import (
	"context"
	"testing"

	"quasarbridge/internal/llm"
)

func TestNativeLogCarriesDecisionsMetadata(t *testing.T) {
	reused, cold := true, false
	ok := decisionsEngine(t, DefaultRoutingConfig(), func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{Route: "llm", Confidence: 0.7, DecisionsCall: llm.DecisionsCall{DurationMS: 240, ConnReused: &reused, RequestID: "req_ok"}}, nil
	}, failingClaude(t))
	line := logLine(t, routeDecisionLogArgs("abcd1234", ok.Decide(t.Context(), "сочини стих")))
	if line["decisions_ms"] != float64(240) || line["conn_reused"] != true {
		t.Fatalf("success line=%v", line)
	}
	if _, found := line["request_id"]; found {
		t.Fatalf("request_id logged on success: %v", line)
	}

	failed := decisionsEngine(t, DefaultRoutingConfig(), func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{}, &llm.DecisionsError{Class: "http", Status: 429, Code: "project_spend_limit_exceeded", DecisionsCall: llm.DecisionsCall{DurationMS: 80, ConnReused: &cold, RequestID: "req_bad"}}
	}, func(context.Context, []llm.Message, bool) (string, error) { return `{"route":"llm"}`, nil })
	d := failed.Decide(t.Context(), "сочини стих")
	line = logLine(t, routeDecisionLogArgs("abcd1234", d))
	want := map[string]any{"backend": "llm", "error_class": "http_429", "error_code": "project_spend_limit_exceeded", "request_id": "req_bad", "decisions_ms": float64(80), "conn_reused": false}
	for k, v := range want {
		if line[k] != v {
			t.Fatalf("%s=%v, want %v; line=%v", k, line[k], v, line)
		}
	}
}
