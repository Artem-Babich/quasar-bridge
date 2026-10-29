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
	"slices"
	"strings"
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
}

// DecisionsError tells the caller why Decisions gave no usable route.
// Class is one of http, transport, timeout, canceled, refusal, bad_response;
// Status is set only for http. The API error message is never kept.
type DecisionsError struct {
	Class  string
	Status int
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/decisions", bytes.NewReader(body))
	if err != nil {
		return RouteAnswer{}, &DecisionsError{Class: "transport"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return RouteAnswer{}, contextOr(ctx, "transport")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return RouteAnswer{}, &DecisionsError{Class: "http", Status: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDecisionsResponseBytes+1))
	if err != nil {
		return RouteAnswer{}, contextOr(ctx, "transport")
	}
	if len(data) > maxDecisionsResponseBytes {
		return RouteAnswer{}, &DecisionsError{Class: "bad_response"}
	}
	var result struct {
		Answers []struct {
			Type       string         `json:"type"`
			Choice     jsontext.Value `json:"choice"`
			Confidence float64        `json:"confidence"`
		} `json:"answers"`
	}
	if err := json.Unmarshal(data, &result); err != nil || len(result.Answers) == 0 {
		return RouteAnswer{}, &DecisionsError{Class: "bad_response"}
	}
	a := result.Answers[0]
	if a.Type == "refusal" {
		return RouteAnswer{}, &DecisionsError{Class: "refusal"}
	}
	var route string
	if a.Type != "choice" || json.Unmarshal(a.Choice, &route) != nil || !slices.Contains(routes, route) {
		return RouteAnswer{}, &DecisionsError{Class: "bad_response"}
	}
	return RouteAnswer{Route: route, Confidence: a.Confidence}, nil
}

func contextOr(ctx context.Context, class string) error {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return &DecisionsError{Class: "timeout"}
	case errors.Is(ctx.Err(), context.Canceled):
		return &DecisionsError{Class: "canceled"}
	}
	return &DecisionsError{Class: class}
}
