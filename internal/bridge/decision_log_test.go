package bridge

// User story: when a phrase is routed by a classifier, the route decision log
// says which classifier decided (backend), how sure it was, how long the step
// took and why the faster classifier failed, so the owner can compare
// Decisions with Claude from real traffic. Decisions made by rules keep the
// log line exactly as before.

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"log/slog"
	"testing"
	"time"

	"quasarbridge/internal/llm"
)

func classifierOnlyEngine(t *testing.T, llmFn completionFunc) *Engine {
	t.Helper()
	cfg := DefaultRoutingConfig()
	cfg.Rules = nil
	e := &Engine{LLM: llmFn}
	if err := e.SetRouting(cfg); err != nil {
		t.Fatal(err)
	}
	return e
}

func logLine(t *testing.T, args []any) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("native_route_decision", args...)
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line %q: %v", buf.String(), err)
	}
	return line
}

func TestClaudeClassifierDecisionReportsBackendLlmAndLatency(t *testing.T) {
	e := classifierOnlyEngine(t, func(context.Context, []llm.Message, bool) (string, error) {
		time.Sleep(20 * time.Millisecond)
		return `{"route":"llm"}`, nil
	})
	d := e.Decide(t.Context(), "расскажи анекдот")
	if d.Route != "llm" || d.Source != "classifier" || d.Backend != "llm" || d.LatencyMS < 20 || d.ErrorClass != "" {
		t.Fatalf("decision=%+v", d)
	}
}

func TestNativeRouteDecisionLogCarriesBackendFields(t *testing.T) {
	d := RouteDecision{Route: "llm", Source: "classifier", Backend: "llm", LatencyMS: 612, ErrorClass: "timeout"}
	line := logLine(t, routeDecisionLogArgs("abcd1234", d))
	want := map[string]any{"request": "abcd1234", "route": "llm", "source": "classifier", "backend": "llm", "latency_ms": float64(612), "error_class": "timeout"}
	for k, v := range want {
		if line[k] != v {
			t.Fatalf("%s=%v, want %v; line=%v", k, line[k], v, line)
		}
	}
	d = RouteDecision{Route: "home", Source: "classifier", Backend: "decisions", Confidence: 0.91, LatencyMS: 240}
	line = logLine(t, routeDecisionLogArgs("abcd1234", d))
	if line["backend"] != "decisions" || line["confidence"] != 0.91 || line["latency_ms"] != float64(240) {
		t.Fatalf("line=%v", line)
	}
	if _, ok := line["error_class"]; ok {
		t.Fatalf("empty error_class logged: %v", line)
	}
}

func TestRuleDecisionsCarryNoBackendFields(t *testing.T) {
	e := &Engine{LLM: completionFunc(func(context.Context, []llm.Message, bool) (string, error) {
		t.Fatal("classifier called for a rule phrase")
		return "", nil
	})}
	d := e.Decide(t.Context(), "спроси нейросеть, сколько весит Луна")
	if d.Source != "rule" || d.Backend != "" || d.LatencyMS != 0 {
		t.Fatalf("decision=%+v", d)
	}
	line := logLine(t, routeDecisionLogArgs("abcd1234", d))
	for _, k := range []string{"backend", "confidence", "latency_ms", "error_class"} {
		if _, ok := line[k]; ok {
			t.Fatalf("rule decision logs %s: %v", k, line)
		}
	}
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("backend")) || bytes.Contains(out, []byte("latency_ms")) {
		t.Fatalf("-route JSON grew fields for a rule decision: %s", out)
	}
}
