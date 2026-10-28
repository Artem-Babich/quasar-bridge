package main

// User story: -check-backends shows the timeout Decisions really gets, which is
// the shorter of decisions_timeout_ms (0 = 1500 ms) and timeout_ms, and whether
// each probe call opened a new connection or reused one. The owner can then
// tell a slow API from a slow TLS handshake after idle.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"quasarbridge/internal/bridge"
	"quasarbridge/internal/llm"
)

func probeServer(t *testing.T) *llm.DecisionsClient {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answers":[{"type":"choice","name":"route","choice":"llm","confidence":0.8}]}`))
	}))
	t.Cleanup(api.Close)
	return &llm.DecisionsClient{BaseURL: api.URL, Model: "m", APIKey: "k", HTTP: &http.Client{Transport: &http.Transport{}}}
}

func TestProbeShowsTimeoutCappedByTimeoutMs(t *testing.T) {
	status, err := checkClassifier(t.Context(), probeServer(t), bridge.ClassifierConfig{Prompt: "p", TimeoutMS: 1000})
	if err != nil || !strings.Contains(status, "(timeout 1000 ms)") {
		t.Fatalf("status=%q err=%v", status, err)
	}
}

func TestProbeShowsNewThenReusedConnection(t *testing.T) {
	status, err := checkClassifier(t.Context(), probeServer(t), bridge.ClassifierConfig{Prompt: "p", TimeoutMS: 8000})
	if err != nil || !strings.Contains(status, " ms new connection, warm ") || !strings.Contains(status, " ms reused connection (timeout") {
		t.Fatalf("status=%q err=%v", status, err)
	}
}
