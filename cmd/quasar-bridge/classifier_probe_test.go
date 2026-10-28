package main

// User story: Go closes idle connections after 90 s, so the first Decisions
// call after a pause is slower. -check-backends measures a cold and a warm
// call and warns when the cold one alone would exceed decisions_timeout_ms,
// so the owner can tell whether the timeout makes Decisions useless after
// idle periods.

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"quasarbridge/internal/bridge"
	"quasarbridge/internal/llm"
)

func TestCheckBackendsReportsColdAndWarmLatency(t *testing.T) {
	out, err := runClassifierChild(t, "check-backends", http.StatusOK)
	pattern := regexp.MustCompile(`LLM: ok; Home Assistant: disabled; Classifier: decisions ok, route (home|llm|yandex), confidence 0\.80, cold \d+ ms, warm \d+ ms \(timeout 1500 ms\)\n`)
	if err != nil || !pattern.MatchString(out) {
		t.Fatalf("err=%v output:\n%s", err, out)
	}
}

func TestCheckBackendsWarnsWhenColdCallExceedsDecisionsTimeout(t *testing.T) {
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			time.Sleep(80 * time.Millisecond)
		}
		_, _ = w.Write([]byte(`{"answers":[{"type":"choice","name":"route","choice":"llm","confidence":0.8}]}`))
	}))
	defer api.Close()
	c := &llm.DecisionsClient{BaseURL: api.URL, Model: "m", APIKey: "k"}
	status, err := checkClassifier(t.Context(), c, bridge.ClassifierConfig{Prompt: "p", TimeoutMS: 8000, DecisionsTimeoutMS: 50})
	if err != nil || !strings.Contains(status, "(timeout 50 ms)") || !strings.Contains(status, "WARNING: cold call exceeds decisions_timeout_ms") {
		t.Fatalf("status=%q err=%v", status, err)
	}
}
