package llm

// User story: the check output names OpenAI's error code (for example a
// spend limit) next to the HTTP status. Codes and request IDs come from the
// network, so they are cut to a short length before they reach every log
// line. Each call reports how long it took, also when it failed.

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDecisionsErrorNamesStatusAndCode(t *testing.T) {
	if got := (&DecisionsError{Class: "http", Status: 429, Code: "project_spend_limit_exceeded"}).Error(); got != "Decisions http 429 project_spend_limit_exceeded" {
		t.Fatalf("error text %q", got)
	}
}

func TestLongErrorCodeAndRequestIdAreCut(t *testing.T) {
	c := decisionsServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", strings.Repeat("r", 500))
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"` + strings.Repeat("c", 500) + `"}}`))
	})
	_, err := c.ChooseRoute(t.Context(), "p", []Message{{Role: "user", Content: "x"}}, testRoutes)
	var de *DecisionsError
	if !errors.As(err, &de) || len(de.Code) > 64 || len(de.RequestID) > 64 || de.Code == "" || de.RequestID == "" {
		t.Fatalf("code=%d bytes request_id=%d bytes", len(de.Code), len(de.RequestID))
	}
}

func TestEveryCallReportsItsDuration(t *testing.T) {
	status := http.StatusOK
	c := decisionsServer(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		decisionsAnswer(w, `{"answers":[{"type":"choice","name":"route","choice":"llm","confidence":0.6}]}`)
	})
	msgs := []Message{{Role: "user", Content: "x"}}
	answer, err := c.ChooseRoute(t.Context(), "p", msgs, testRoutes)
	if err != nil || answer.DurationMS < 20 {
		t.Fatalf("success duration=%d err=%v", answer.DurationMS, err)
	}
	status = http.StatusInternalServerError
	_, err = c.ChooseRoute(t.Context(), "p", msgs, testRoutes)
	var de *DecisionsError
	if !errors.As(err, &de) || de.DurationMS < 20 {
		t.Fatalf("failure duration=%+v", de)
	}
}
