package llm

// User story: to tell a slow cold connection from a slow model, and a spend
// limit from an outage, the owner needs each Decisions call's duration,
// whether the connection was reused, OpenAI's error code and request ID.
// OpenAI's free-text error message is never kept: it can echo input.

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestResponseReportsRequestIdAndConnReuse(t *testing.T) {
	c := decisionsServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req_123")
		decisionsAnswer(w, `{"answers":[{"type":"choice","name":"route","choice":"llm","confidence":0.6}]}`)
	})
	msgs := []Message{{Role: "user", Content: "x"}}
	first, err := c.ChooseRoute(t.Context(), "p", msgs, testRoutes)
	if err != nil || first.RequestID != "req_123" || first.ConnReused == nil || *first.ConnReused || first.DurationMS < 0 {
		t.Fatalf("first=%+v reused=%v err=%v", first, first.ConnReused, err)
	}
	second, err := c.ChooseRoute(t.Context(), "p", msgs, testRoutes)
	if err != nil || second.ConnReused == nil || !*second.ConnReused {
		t.Fatalf("second call did not reuse the connection: %+v err=%v", second, err)
	}
}

func TestErrorCodeIsKeptButMessageIsNot(t *testing.T) {
	c := decisionsServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req_9")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"project_spend_limit_exceeded","message":"secret phrase text"}}`))
	})
	_, err := c.ChooseRoute(t.Context(), "p", []Message{{Role: "user", Content: "x"}}, testRoutes)
	var de *DecisionsError
	if !errors.As(err, &de) || de.Class != "http" || de.Status != 429 || de.Code != "project_spend_limit_exceeded" || de.RequestID != "req_9" || de.ConnReused == nil {
		t.Fatalf("error=%+v", de)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(de.Code+de.RequestID, "secret") {
		t.Fatalf("API message leaked: %v", err)
	}
}
