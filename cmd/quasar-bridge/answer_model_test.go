package main

// User story: the owner sets llm_answer_model (or QUASAR_LLM_ANSWER_MODEL) so
// spoken answers use a stronger model on the same API and key. The main client
// keeps llm_model for home commands and the backup route choice. An unset
// answer model changes nothing.

import (
	"context"
	"encoding/json/v2"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"quasarbridge/internal/llm"
)

func TestAnswerModelIsReadFromConfigAndEnvironment(t *testing.T) {
	cfg, err := readConfig(configFile(t, `{"llm_answer_model":"strong"}`))
	if err != nil || cfg.LLMAnswerModel != "strong" {
		t.Fatalf("model=%q err=%v", cfg.LLMAnswerModel, err)
	}
	t.Setenv("QUASAR_LLM_ANSWER_MODEL", "stronger")
	cfg, err = readConfig(configFile(t, `{"llm_answer_model":"strong"}`))
	if err != nil || cfg.LLMAnswerModel != "stronger" {
		t.Fatalf("model=%q err=%v", cfg.LLMAnswerModel, err)
	}
}

func TestAnswerClientDiffersFromMainClientOnlyInModel(t *testing.T) {
	main := &llm.Client{BaseURL: "https://llm.example.invalid/v1", Model: "fast", APIKey: "fixture-key", API: "chat_completions", ReasoningEffort: "low"}
	answers := answerClient(main, "strong")
	if answers == main || answers.Model != "strong" {
		t.Fatalf("answers=%+v", answers)
	}
	copied := *answers
	copied.Model = "fast"
	if copied.BaseURL != main.BaseURL || copied.APIKey != main.APIKey || copied.API != main.API || copied.ReasoningEffort != main.ReasoningEffort {
		t.Fatalf("answers=%+v main=%+v", answers, main)
	}
	if main.Model != "fast" {
		t.Fatalf("main client changed: %+v", main)
	}
}

func TestUnsetAnswerModelKeepsTheMainClient(t *testing.T) {
	main := &llm.Client{Model: "fast"}
	if answers := answerClient(main, ""); answers != main {
		t.Fatalf("answers=%+v", answers)
	}
}

func TestAnswerModelProcess(t *testing.T) {
	if action := os.Getenv("QUASAR_ANSWER_CHILD"); action != "" {
		os.Args = []string{"quasar-bridge", "-config", os.Getenv("QUASAR_ANSWER_CONFIG"), "-" + action}
		if action == "query" {
			os.Args = append(os.Args, "сколько варить гречку")
		}
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		main()
		return
	}
	var mu sync.Mutex
	var models []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.UnmarshalRead(r.Body, &body, json.RejectUnknownMembers(false)); err != nil {
			t.Error(err)
		}
		mu.Lock()
		models = append(models, body.Model)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Десять минут."}}]}`))
	}))
	defer api.Close()
	settings := map[string]any{"mode": "hybrid", "listen": "localhost:0", "llm_base_url": api.URL + "/v1", "llm_model": "fast", "llm_answer_model": "strong",
		"routing": map[string]any{"rules": []any{}, "default_route": "llm"}}
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	path := configFile(t, string(raw))
	for _, tc := range []struct {
		action string
		want   []string
	}{
		{"query", []string{"strong"}},
		{"check-backends", []string{"fast", "strong"}},
	} {
		t.Run(tc.action, func(t *testing.T) {
			mu.Lock()
			models = nil
			mu.Unlock()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAnswerModelProcess$")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "QUASAR_") {
					child.Env = append(child.Env, entry)
				}
			}
			child.Env = append(child.Env, "QUASAR_HOME_MCP_URL=", "QUASAR_ANSWER_CHILD="+tc.action, "QUASAR_ANSWER_CONFIG="+path)
			if output, err := child.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, output)
			}
			mu.Lock()
			defer mu.Unlock()
			if !slices.Equal(models, tc.want) {
				t.Fatalf("models=%v, want %v", models, tc.want)
			}
		})
	}
}
