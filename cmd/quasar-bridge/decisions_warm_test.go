package main

// User story: with the Decisions classifier on, the running bridge keeps the
// connection to OpenAI warm from the moment it starts, so the first phrase
// after a pause is not slow. With Decisions off no keeper starts, and the
// short commands (-route, -check-backends) never start the keeper: the
// fake API in the classifier process tests rejects any request but its own.

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func serveWithFakeOpenAI(t *testing.T, ctx context.Context, settings map[string]any, handler http.HandlerFunc) {
	t.Helper()
	api := httptest.NewServer(handler)
	t.Cleanup(api.Close)
	settings["mode"] = "hybrid"
	settings["listen"] = "localhost:0"
	settings["llm_base_url"] = api.URL + "/v1"
	settings["llm_model"] = "fixture"
	if settings["classifier_api"] == "decisions" {
		settings["classifier_base_url"] = api.URL + "/v1"
	}
	body, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestClassifierServerChild$")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "QUASAR_") {
			child.Env = append(child.Env, entry)
		}
	}
	child.Env = append(child.Env, "QUASAR_CLASSIFIER_SERVER_CHILD=1", "QUASAR_CLASSIFIER_CONFIG="+configFile(t, string(body)), "QUASAR_CLASSIFIER_API_KEY=fixture-key")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Wait() })
}

func TestRunningBridgeWarmsTheDecisionsConnectionAtStart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	warmed := make(chan string, 16)
	serveWithFakeOpenAI(t, ctx, map[string]any{"classifier_api": "decisions", "classifier_model": "fixture-decisions"}, func(_ http.ResponseWriter, r *http.Request) {
		warmed <- r.Method + " " + r.URL.Path
	})
	select {
	case got := <-warmed:
		if got != "GET /v1/models/fixture-decisions" {
			t.Fatalf("first request %q", got)
		}
	case <-ctx.Done():
		t.Fatal("running bridge never warmed the connection")
	}
}

func TestWithDecisionsOffNoKeeperStarts(t *testing.T) {
	keepDecisionsWarm(t.Context(), nil)
	// A keeper on the missing client would crash this process here.
	time.Sleep(50 * time.Millisecond)
}
