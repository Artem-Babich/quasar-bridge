package llm

// User story: when -check-backends fails, the operator must see why: a bad
// key (401), a wrong model or URL (404) and a rate limit (429) read
// differently. A huge response is rejected even if it starts with a valid
// answer, so a broken proxy cannot make the bridge buffer it.

import (
	"net/http"
	"strings"
	"testing"
)

func TestDecisionsErrorNamesHTTPStatus(t *testing.T) {
	if got := (&DecisionsError{Class: "http", Status: 401}).Error(); got != "Decisions http 401" {
		t.Fatalf("error text %q", got)
	}
	if got := (&DecisionsError{Class: "refusal"}).Error(); got != "Decisions refusal" {
		t.Fatalf("error text %q", got)
	}
}

func TestOversizedValidAnswerIsRejected(t *testing.T) {
	body := `{"answers":[{"type":"choice","name":"route","choice":"llm","confidence":0.9}],"pad":"` + strings.Repeat("x", 70<<10) + `"}`
	c := decisionsServer(t, func(w http.ResponseWriter, r *http.Request) { decisionsAnswer(w, body) })
	_, err := c.ChooseRoute(t.Context(), "p", []Message{{Role: "user", Content: "x"}}, testRoutes)
	requireClass(t, err, "bad_response", 0)
}
