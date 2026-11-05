package dialogue

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"quasarbridge/internal/llm"
)

// Fork: limits of the record the route classifiers read.
const (
	routeContextTurns  = 3
	routeContextWindow = 2 * time.Minute
)

// routeTurn is one delivered exchange of any route. It is separate from turn:
// the answer dialogue stays Claude's own and is cleared by non-LLM routes.
type routeTurn struct {
	user, answer, route string
	private             bool
	at                  time.Time
}

func (s *Store) recentRouteTurns(e *entry) []routeTurn {
	first := max(0, len(e.routeTurns)-routeContextTurns)
	for first < len(e.routeTurns) && s.now().Sub(e.routeTurns[first].at) >= routeContextWindow {
		first++
	}
	return e.routeTurns[first:]
}

// RouteContext returns what the route classifiers may read for this speaker,
// each reply tagged with the route that gave it.
func (s *Store) RouteContext(ctx context.Context) Snapshot {
	var v Snapshot
	if s == nil || !s.cfg.Enabled {
		return v
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[key(ctx)]
	if e == nil || s.expired(e) {
		return v
	}
	for _, t := range s.recentRouteTurns(e) {
		v.Messages = append(v.Messages, llm.Message{Role: "user", Content: t.user}, llm.Message{Role: "assistant", Content: "[" + t.route + "] " + t.answer})
		v.PrivateHistory = v.PrivateHistory || t.private
	}
	return v
}

// CaptureRouteTurn is called only after a reply was delivered. Streaming parts
// wait for the final one, like CaptureReplay; a reply too long for the dialogue
// limit is not kept.
func (s *Store) CaptureRouteTurn(ctx context.Context, ticket Ticket, route, question, text string, final, private bool) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.current(ctx, ticket) {
		return
	}
	e := s.entries[ticket.key]
	if e.routeDone {
		return
	}
	if strings.HasPrefix(text, e.routePartial) {
		e.routePartial = text
	} else {
		e.routePartial += text
	}
	oversized := utf8.RuneCountInString(e.routePartial)+utf8.RuneCountInString(question) > s.cfg.MaxChars
	if !final && !oversized {
		return
	}
	answer := e.routePartial
	e.routePartial = ""
	e.routeDone = true
	if oversized || strings.TrimSpace(answer) == "" || strings.TrimSpace(question) == "" {
		return
	}
	e.routeTurns = append(slices.Clone(s.recentRouteTurns(e)), routeTurn{user: question, answer: answer, route: route, private: private, at: s.now()})
	e.routeTurns = e.routeTurns[max(0, len(e.routeTurns)-routeContextTurns):]
}

// ClearRouteContext forgets the record if this request is still the latest one.
func (s *Store) ClearRouteContext(ctx context.Context, ticket Ticket) bool {
	if s == nil || !s.cfg.Enabled {
		return ctx.Err() == nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.current(ctx, ticket) {
		return false
	}
	e := s.entries[ticket.key]
	e.routeTurns = nil
	e.routePartial = ""
	e.routeDone = true
	return true
}
