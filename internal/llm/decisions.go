package llm

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"slices"
	"strings"
	"time"
)

// DecisionsClient asks the OpenAI Decisions API to pick one route.
type DecisionsClient struct {
	BaseURL string
	Model   string
	APIKey  string
	HTTP    *http.Client
}

type RouteAnswer struct {
	Route      string
	Confidence float64
	DecisionsCall
}

// DecisionsCall describes one HTTP exchange for the route decision log.
type DecisionsCall struct {
	DurationMS int64
	ConnReused *bool
	RequestID  string
}

// DecisionsError tells the caller why Decisions gave no usable route.
// Class is one of http, transport, timeout, canceled, refusal, bad_response;
// Status is set only for http. The API error message is never kept.
type DecisionsError struct {
	Class  string
	Status int
	Code   string // OpenAI error.code, if the body had one
	DecisionsCall
}

func (e *DecisionsError) Error() string {
	if e.Class == "http" {
		return fmt.Sprintf("Decisions http %d", e.Status)
	}
	return "Decisions " + e.Class
}

const maxDecisionsResponseBytes = 64 << 10

type decisionsInput struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type decisionsChoice struct {
	Value string `json:"value"`
}
type decisionsQuestion struct {
	Type         string            `json:"type"`
	Name         string            `json:"name"`
	Instructions string            `json:"instructions"`
	Choices      []decisionsChoice `json:"choices"`
}

// ChooseRoute sends one choice question over routes. The caller's context
// bounds the call; there are no retries.
func (c *DecisionsClient) ChooseRoute(ctx context.Context, instructions string, messages []Message, routes []string) (RouteAnswer, error) {
	input := make([]decisionsInput, 0, len(messages))
	for _, m := range messages {
		input = append(input, decisionsInput{Role: m.Role, Content: m.Content})
	}
	choices := make([]decisionsChoice, 0, len(routes))
	for _, r := range routes {
		choices = append(choices, decisionsChoice{Value: r})
	}
	body, err := json.Marshal(map[string]any{
		"model":     c.Model,
		"input":     input,
		"questions": []decisionsQuestion{{Type: "choice", Name: "route", Instructions: instructions, Choices: choices}},
	})
	if err != nil {
		return RouteAnswer{}, &DecisionsError{Class: "bad_response"}
	}
	var call DecisionsCall
	began := time.Now()
	fail := func(e *DecisionsError) (RouteAnswer, error) {
		call.DurationMS = time.Since(began).Milliseconds()
		e.DecisionsCall = call
		return RouteAnswer{}, e
	}
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
		reused := info.Reused
		call.ConnReused = &reused
	}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/decisions", bytes.NewReader(body))
	if err != nil {
		return fail(&DecisionsError{Class: "transport"})
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fail(contextOr(ctx, "transport"))
	}
	defer resp.Body.Close()
	call.RequestID = resp.Header.Get("X-Request-Id")
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDecisionsResponseBytes+1))
	if resp.StatusCode != http.StatusOK {
		// Keep only the machine-readable code; the message can echo input.
		var apiError struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &apiError)
		return fail(&DecisionsError{Class: "http", Status: resp.StatusCode, Code: apiError.Error.Code})
	}
	if err != nil {
		return fail(contextOr(ctx, "transport"))
	}
	if len(data) > maxDecisionsResponseBytes {
		return fail(&DecisionsError{Class: "bad_response"})
	}
	var result struct {
		Answers []struct {
			Type       string         `json:"type"`
			Choice     jsontext.Value `json:"choice"`
			Confidence float64        `json:"confidence"`
		} `json:"answers"`
	}
	if err := json.Unmarshal(data, &result); err != nil || len(result.Answers) == 0 {
		return fail(&DecisionsError{Class: "bad_response"})
	}
	a := result.Answers[0]
	if a.Type == "refusal" {
		return fail(&DecisionsError{Class: "refusal"})
	}
	var route string
	if a.Type != "choice" || json.Unmarshal(a.Choice, &route) != nil || !slices.Contains(routes, route) {
		return fail(&DecisionsError{Class: "bad_response"})
	}
	call.DurationMS = time.Since(began).Milliseconds()
	return RouteAnswer{Route: route, Confidence: a.Confidence, DecisionsCall: call}, nil
}

func contextOr(ctx context.Context, class string) *DecisionsError {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return &DecisionsError{Class: "timeout"}
	case errors.Is(ctx.Err(), context.Canceled):
		return &DecisionsError{Class: "canceled"}
	}
	return &DecisionsError{Class: class}
}
