package bridge

// User story: the owner wants spoken answers from a stronger model, while home
// commands and the backup route choice stay on the fast model, because those
// run before the speaker says anything. Without a separate answer model the
// bridge behaves as before: one model does everything.

import (
	"context"
	"testing"

	"quasarbridge/internal/llm"
)

func modelSpy(name string, calls *[]string, answer string) completionFunc {
	return func(context.Context, []llm.Message, bool) (string, error) {
		*calls = append(*calls, name)
		return answer, nil
	}
}

func answerEngine(t *testing.T, calls *[]string, classification string) *Engine {
	t.Helper()
	engine := &Engine{LLM: modelSpy("fast", calls, classification), Answers: modelSpy("strong", calls, "Десять минут.")}
	cfg := DefaultRoutingConfig()
	cfg.Rules = nil
	cfg.DefaultRoute = "classify"
	if err := engine.SetRouting(cfg); err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestQuestionIsAnsweredByTheAnswerModel(t *testing.T) {
	var calls []string
	engine := answerEngine(t, &calls, `{"route":"llm"}`)
	r, err := engine.Resolve(t.Context(), "сколько варить гречку")
	if err != nil || r.Route != "llm" || r.Answer != "Десять минут." {
		t.Fatalf("resolution=%+v err=%v", r, err)
	}
	if len(calls) != 2 || calls[0] != "fast" || calls[1] != "strong" {
		t.Fatalf("route choice must stay on the fast model, the answer must use the strong one: %v", calls)
	}
}

func TestRepeatedQuestionIsAnsweredByTheAnswerModel(t *testing.T) {
	var calls []string
	engine := answerEngine(t, &calls, `{"safe":true}`)
	ctx := t.Context()
	engine.RecordDelivered(ctx, "сколько варить гречку", "Не знаю.", true, false)
	calls = nil
	r, err := engine.ResolveDecision(ctx, "используй нейросеть", RouteDecision{Route: "llm"})
	if err != nil || r.Answer != "Десять минут." {
		t.Fatalf("resolution=%+v err=%v", r, err)
	}
	if len(calls) == 0 || calls[len(calls)-1] != "strong" {
		t.Fatalf("the repeated answer must use the strong model: %v", calls)
	}
}

func TestWithoutAnswerModelOneModelDoesEverything(t *testing.T) {
	var calls []string
	engine := &Engine{LLM: modelSpy("only", &calls, "Десять минут.")}
	cfg := DefaultRoutingConfig()
	cfg.Rules = nil
	cfg.DefaultRoute = "llm"
	if err := engine.SetRouting(cfg); err != nil {
		t.Fatal(err)
	}
	r, err := engine.Resolve(t.Context(), "сколько варить гречку")
	if err != nil || r.Answer != "Десять минут." || len(calls) != 1 || calls[0] != "only" {
		t.Fatalf("resolution=%+v calls=%v err=%v", r, calls, err)
	}
}
