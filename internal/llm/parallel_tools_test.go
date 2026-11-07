package llm

// User story: Claude writes a whole shopping list. One step per item would run
// out of steps, so a deployment may let the model call several tools in one
// step. Without that setting the bridge keeps one call per step.

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"
)

func sentParallelFlag(t *testing.T, api string, allowed bool) any {
	t.Helper()
	var sent any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.UnmarshalRead(r.Body, &body); err != nil {
			t.Error(err)
		}
		sent = body["parallel_tool_calls"]
		if api == "responses" {
			_, _ = w.Write([]byte(responseAnswer))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer s.Close()
	c := &Client{BaseURL: s.URL, Model: "m", API: api, ParallelTools: allowed, HTTP: s.Client()}
	tool := Tool{Type: "function"}
	tool.Function.Name = "add_item"
	if _, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, []Tool{tool}); err != nil {
		t.Fatal(err)
	}
	return sent
}

func TestParallelToolsLetTheModelCallSeveralToolsInOneStep(t *testing.T) {
	for _, api := range []string{"chat_completions", "responses"} {
		t.Run(api, func(t *testing.T) {
			if got := sentParallelFlag(t, api, true); got != true {
				t.Fatalf("parallel_tool_calls=%v", got)
			}
		})
	}
}

func TestWithoutTheSettingTheModelKeepsOneToolCallPerStep(t *testing.T) {
	for _, api := range []string{"chat_completions", "responses"} {
		t.Run(api, func(t *testing.T) {
			if got := sentParallelFlag(t, api, false); got != false {
				t.Fatalf("parallel_tool_calls=%v", got)
			}
		})
	}
}
