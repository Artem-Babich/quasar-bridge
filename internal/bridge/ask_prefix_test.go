package bridge

// User story: the owner says «спроси нейросеть: сколько весит Луна». The words
// «спроси нейросеть» only choose the handler, so the answer model must get the
// bare question. The same holds when the question is repeated later with
// «используй нейросеть». A bare «спроси нейросеть» has nothing to strip and
// stays as it is. The prefix counts only at the start of the phrase.

import (
	"context"
	"testing"

	"quasarbridge/internal/llm"
)

func TestAskModelPrefixIsRemovedOnlyFromTheStartOfANewQuestion(t *testing.T) {
	for text, want := range map[string]string{
		"спроси нейросеть: сколько весит Луна":       "сколько весит Луна",
		"Спроси нейросеть, сколько весит Луна?":      "сколько весит Луна?",
		"Алиса, спроси нейросеть сколько весит Луна": "сколько весит Луна",
		"спроси нейросеть":                           "спроси нейросеть",
		"спроси нейросеть.":                          "спроси нейросеть.",
		"как спроси нейросеть работает":              "как спроси нейросеть работает",
		"спроси нейросетью":                          "спроси нейросетью",
		"сколько весит Луна":                         "сколько весит Луна",
	} {
		if got := stripAskModelPrefix(text); got != want {
			t.Errorf("%q: got %q, want %q", text, got, want)
		}
	}
}

func TestAnswerModelGetsTheBareQuestion(t *testing.T) {
	var got []llm.Message
	e := &Engine{LLM: completionFunc(func(_ context.Context, m []llm.Message, _ bool) (string, error) {
		got = m
		return "Ответ", nil
	})}
	if _, err := e.Resolve(t.Context(), "спроси нейросеть: сколько весит Луна"); err != nil {
		t.Fatal(err)
	}
	if last := got[len(got)-1]; last.Role != "user" || last.Content != "сколько весит Луна" {
		t.Fatalf("answer model got %+v", last)
	}
}

func TestRepeatedQuestionReachesTheModelWithoutThePrefix(t *testing.T) {
	var got []llm.Message
	e := &Engine{LLM: completionFunc(func(_ context.Context, m []llm.Message, structured bool) (string, error) {
		if structured {
			return `{"safe":true}`, nil
		}
		got = m
		return "Ответ", nil
	})}
	e.RecordDelivered(t.Context(), "спроси нейросеть: сколько весит Луна", "Не знаю.", true, false)
	if _, err := e.Resolve(t.Context(), "используй нейросеть"); err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[len(got)-1].Content != "сколько весит Луна" {
		t.Fatalf("answer model got %+v", got)
	}
}
