package main

// User story: after enabling the Decisions classifier the owner runs
// -check-backends and sees, at the end of the usual line, whether Decisions
// answers; a broken Decisions setup fails the check like a broken LLM does.
// -route shows which classifier decided. With Decisions off the check line is
// exactly as before.

import (
	"context"
	"encoding/json/v2"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestClassifierProcessChild(t *testing.T) {
	action := os.Getenv("QUASAR_CLASSIFIER_CHILD")
	if action == "" {
		t.Skip("child process helper")
	}
	flag.CommandLine = flag.NewFlagSet("quasar-bridge", flag.ExitOnError)
	os.Args = []string{"quasar-bridge", "-config", os.Getenv("QUASAR_CLASSIFIER_CONFIG"), "-" + action}
	if action == "route" {
		os.Args = append(os.Args, "расскажи анекдот")
	}
	main()
}

// runClassifierChild runs the bridge with a fake OpenAI-compatible API.
// decisionsStatus is the HTTP status of /v1/decisions; 0 disables Decisions.
func runClassifierChild(t *testing.T, action string, decisionsStatus int) (string, error) {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"route\":\"yandex\"}"}}]}`))
		case "/v1/decisions":
			if decisionsStatus != http.StatusOK {
				http.Error(w, "{}", decisionsStatus)
				return
			}
			_, _ = w.Write([]byte(`{"answers":[{"type":"choice","name":"route","choice":"llm","confidence":0.8}]}`))
		default:
			t.Errorf("unexpected API request %s", r.URL.Path)
		}
	}))
	defer api.Close()
	settings := map[string]any{"mode": "hybrid", "listen": "localhost:0", "llm_base_url": api.URL + "/v1", "llm_model": "fixture"}
	if decisionsStatus != 0 {
		settings["classifier_api"] = "decisions"
		settings["classifier_base_url"] = api.URL + "/v1"
		settings["classifier_model"] = "fixture-decisions"
	}
	body, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestClassifierProcessChild$")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "QUASAR_") {
			child.Env = append(child.Env, entry)
		}
	}
	child.Env = append(child.Env, "QUASAR_CLASSIFIER_CHILD="+action, "QUASAR_CLASSIFIER_CONFIG="+configFile(t, string(body)), "QUASAR_CLASSIFIER_API_KEY=fixture-key")
	out, err := child.CombinedOutput()
	return string(out), err
}

func TestCheckBackendsAppendsClassifierStatus(t *testing.T) {
	out, err := runClassifierChild(t, "check-backends", http.StatusOK)
	if err != nil || !strings.Contains(out, "LLM: ok; Home Assistant: disabled; Classifier: decisions ok") {
		t.Fatalf("err=%v output:\n%s", err, out)
	}
}

func TestCheckBackendsFailsWhenDecisionsFails(t *testing.T) {
	out, err := runClassifierChild(t, "check-backends", http.StatusUnauthorized)
	if err == nil || strings.Contains(out, "Classifier: decisions ok") {
		t.Fatalf("broken Decisions passed the check: err=%v output:\n%s", err, out)
	}
}

func TestRouteExplainsDecisionsBackend(t *testing.T) {
	out, err := runClassifierChild(t, "route", http.StatusOK)
	if err != nil || !strings.Contains(out, `"route":"llm"`) || !strings.Contains(out, `"backend":"decisions"`) {
		t.Fatalf("err=%v output:\n%s", err, out)
	}
}

func TestCheckBackendsUnchangedWhenDecisionsOff(t *testing.T) {
	out, err := runClassifierChild(t, "check-backends", 0)
	if err != nil || !strings.Contains(out, "LLM: ok; Home Assistant: disabled\n") || strings.Contains(out, "Classifier") {
		t.Fatalf("err=%v output:\n%s", err, out)
	}
}
