package bridge

// User story: Decisions sends a shopping request to Claude, and Claude keeps the
// list through ha-mcp. The bridge matches no phrases: it only tells Claude how
// to keep the list, and only when Claude has the tools for it.

import (
	"context"
	"strings"
	"testing"

	"quasarbridge/internal/llm"
)

type systemPromptCapture struct{ system string }

func (c *systemPromptCapture) Complete(_ context.Context, m []llm.Message, _ bool) (string, error) {
	c.system = m[0].Content
	return "Записал.", nil
}

func TestWithTheListToolsClaudeIsToldHowToKeepTheList(t *testing.T) {
	for _, rule := range []string{"todo.shopping_list", "needs_action", "completed", "одном шаге", "не больше 8", "приложении", "«Это»"} {
		capture := &systemPromptCapture{}
		e := &Engine{LLM: capture, ShoppingByTools: true}
		if _, err := e.Fallback(context.Background(), "купи хлеб"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(capture.system, rule) {
			t.Fatalf("no rule %q in %q", rule, capture.system)
		}
	}
}

func TestWithoutTheListToolsClaudeHearsNothingAboutTheList(t *testing.T) {
	capture := &systemPromptCapture{}
	e := &Engine{LLM: capture}
	if _, err := e.Fallback(context.Background(), "купи хлеб"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(capture.system, "todo.shopping_list") {
		t.Fatalf("list rules without tools: %q", capture.system)
	}
}
