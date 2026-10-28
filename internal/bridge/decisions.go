package bridge

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"quasarbridge/internal/dialogue"
	"quasarbridge/internal/llm"
)

// RouteChooser picks a route without the answering LLM (OpenAI Decisions).
type RouteChooser interface {
	ChooseRoute(ctx context.Context, instructions string, messages []llm.Message, routes []string) (llm.RouteAnswer, error)
}

var _ RouteChooser = (*llm.DecisionsClient)(nil)

// DecisionsTimeout is decisions_timeout_ms, or 1500 ms when it is 0.
func (c ClassifierConfig) DecisionsTimeout() time.Duration {
	if c.DecisionsTimeoutMS > 0 {
		return time.Duration(c.DecisionsTimeoutMS) * time.Millisecond
	}
	return 1500 * time.Millisecond
}

// chooseWithDecisions asks Decisions within decisions_timeout_ms. The caller's
// context already caps the whole classifier step at timeout_ms.
func (e *Engine) chooseWithDecisions(ctx context.Context, c ClassifierConfig, snapshot dialogue.Snapshot, text string) (RouteDecision, error) {
	ctx, cancel := context.WithTimeout(ctx, c.DecisionsTimeout())
	defer cancel()
	var messages []llm.Message
	if !snapshot.PrivateHistory {
		messages = slices.Clone(snapshot.Messages)
	}
	messages = append(messages, llm.Message{Role: "user", Content: text})
	answer, err := e.Decisions.ChooseRoute(ctx, c.Prompt, messages, []string{"home", "llm", "yandex"})
	if err != nil {
		failure := RouteDecision{ErrorClass: decisionsErrorClass(err)}
		var de *llm.DecisionsError
		if errors.As(err, &de) {
			failure.DecisionsMS, failure.ConnReused = de.DurationMS, de.ConnReused
			failure.ErrorCode, failure.RequestID = de.Code, de.RequestID
		}
		return failure, err
	}
	return RouteDecision{Route: answer.Route, Source: "classifier", Backend: "decisions", Confidence: answer.Confidence,
		DecisionsMS: answer.DurationMS, ConnReused: answer.ConnReused}, nil
}

// keepDecisionsFailure copies why Decisions failed onto the decision the
// Claude classifier made instead.
func (d *RouteDecision) keepDecisionsFailure(f RouteDecision) {
	d.ErrorClass, d.ErrorCode, d.RequestID = f.ErrorClass, f.ErrorCode, f.RequestID
	d.DecisionsMS, d.ConnReused = f.DecisionsMS, f.ConnReused
}

// decisionsErrorClass names a Decisions failure for the route decision log.
func decisionsErrorClass(err error) string {
	var de *llm.DecisionsError
	if !errors.As(err, &de) {
		return "transport"
	}
	if de.Class == "http" {
		return fmt.Sprintf("http_%d", de.Status)
	}
	return de.Class
}
