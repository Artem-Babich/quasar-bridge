package llm

// User story: the bridge can ask the OpenAI Decisions API which route a
// phrase takes (home, llm or yandex). The request carries the same classifier
// prompt and dialogue history as the Claude classifier, and every way the API
// can fail is reported as a distinct class so the bridge can fall back to
// Claude and log why.

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var testRoutes = []string{"home", "llm", "yandex"}

func decisionsServer(t *testing.T, handler http.HandlerFunc) *DecisionsClient {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	return &DecisionsClient{BaseURL: s.URL + "/v1", Model: "fixture-model", APIKey: "fixture-key"}
}

func decisionsAnswer(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func requireClass(t *testing.T, err error, class string, status int) {
	t.Helper()
	var de *DecisionsError
	if !errors.As(err, &de) || de.Class != class || de.Status != status {
		t.Fatalf("error=%v, want class %q status %d", err, class, status)
	}
}

func TestDecisionsRequestCarriesPromptHistoryAndThreeRouteChoices(t *testing.T) {
	type input struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type choice struct {
		Value string `json:"value"`
	}
	type question struct {
		Type         string   `json:"type"`
		Name         string   `json:"name"`
		Instructions string   `json:"instructions"`
		Choices      []choice `json:"choices"`
	}
	var got struct {
		Model     string     `json:"model"`
		Input     []input    `json:"input"`
		Questions []question `json:"questions"`
	}
	var path, auth string
	c := decisionsServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		if err := json.UnmarshalRead(r.Body, &got); err != nil {
			http.Error(w, "bad payload", 400)
			return
		}
		decisionsAnswer(w, `{"answers":[{"type":"choice","name":"route","choice":"home","confidence":0.93,"probabilities":[]}],"model":"fixture-model"}`)
	})
	history := []Message{{Role: "user", Content: "включи свет"}, {Role: "assistant", Content: "Включила."}, {Role: "user", Content: "а теперь выключи"}}
	answer, err := c.ChooseRoute(t.Context(), "classifier prompt", history, testRoutes)
	if err != nil || answer.Route != "home" || answer.Confidence != 0.93 {
		t.Fatalf("answer=%+v error=%v", answer, err)
	}
	if path != "/v1/decisions" || auth != "Bearer fixture-key" || got.Model != "fixture-model" {
		t.Fatalf("path=%q auth=%q model=%q", path, auth, got.Model)
	}
	if len(got.Input) != 3 || got.Input[0] != (input{"user", "включи свет"}) || got.Input[1] != (input{"assistant", "Включила."}) || got.Input[2] != (input{"user", "а теперь выключи"}) {
		t.Fatalf("input=%+v", got.Input)
	}
	if len(got.Questions) != 1 {
		t.Fatalf("questions=%+v", got.Questions)
	}
	q := got.Questions[0]
	if q.Type != "choice" || q.Name != "route" || q.Instructions != "classifier prompt" || len(q.Choices) != 3 || q.Choices[0].Value != "home" || q.Choices[1].Value != "llm" || q.Choices[2].Value != "yandex" {
		t.Fatalf("question=%+v", q)
	}
}

func TestNon200AnswerIsClassifiedHttpWithStatus(t *testing.T) {
	for _, status := range []int{401, 429, 500} {
		calls := 0
		c := decisionsServer(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			http.Error(w, `{"error":{"message":"secret detail"}}`, status)
		})
		_, err := c.ChooseRoute(t.Context(), "p", []Message{{Role: "user", Content: "x"}}, testRoutes)
		requireClass(t, err, "http", status)
		if calls != 1 {
			t.Fatalf("status %d: %d calls, want exactly one (no retry)", status, calls)
		}
		if strings.Contains(err.Error(), "secret detail") {
			t.Fatalf("error leaks API message: %v", err)
		}
	}
}

func TestRefusalUnknownChoiceBadJSONAndOversizedBodyAreRejected(t *testing.T) {
	cases := map[string]struct {
		body  string
		class string
	}{
		"refusal":        {`{"answers":[{"type":"refusal","name":"route"}]}`, "refusal"},
		"unknown choice": {`{"answers":[{"type":"choice","name":"route","choice":"weather","confidence":0.9}]}`, "bad_response"},
		"boolean choice": {`{"answers":[{"type":"choice","name":"route","choice":true,"confidence":0.9}]}`, "bad_response"},
		"no answers":     {`{"answers":[]}`, "bad_response"},
		"bad json":       {`{"answers":`, "bad_response"},
		"oversized":      {`{"answers":[],"pad":"` + strings.Repeat("x", 70<<10) + `"}`, "bad_response"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := decisionsServer(t, func(w http.ResponseWriter, r *http.Request) { decisionsAnswer(w, tc.body) })
			_, err := c.ChooseRoute(t.Context(), "p", []Message{{Role: "user", Content: "x"}}, testRoutes)
			requireClass(t, err, tc.class, 0)
		})
	}
}

func TestTimeoutTransportAndCancelAreDistinguished(t *testing.T) {
	slow := decisionsServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	})
	msgs := []Message{{Role: "user", Content: "x"}}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, err := slow.ChooseRoute(ctx, "p", msgs, testRoutes)
	requireClass(t, err, "timeout", 0)

	ctx, cancel = context.WithCancel(t.Context())
	time.AfterFunc(30*time.Millisecond, cancel)
	_, err = slow.ChooseRoute(ctx, "p", msgs, testRoutes)
	requireClass(t, err, "canceled", 0)

	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()
	c := &DecisionsClient{BaseURL: url, Model: "m", APIKey: "k"}
	_, err = c.ChooseRoute(t.Context(), "p", msgs, testRoutes)
	requireClass(t, err, "transport", 0)
}
