// Package ytmusic drives ytm-alice, the YouTube Music player that runs on the
// speaker, through its panel API. The bridge understands the phrase; the
// player only plays.
package ytmusic

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client talks to the ytm-alice panel. The panel accepts commands only with
// its own Origin and the X-Ytm-Client header.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

type Track struct {
	Title   string `json:"title"`
	Artists string `json:"artists"`
}

type State struct {
	Status string  `json:"status"`
	Queue  []Track `json:"queue"`
	Index  int     `json:"index"`
}

// Current returns the queued track the player is on.
func (s State) Current() (Track, bool) {
	if s.Index < 0 || s.Index >= len(s.Queue) {
		return Track{}, false
	}
	return s.Queue[s.Index], true
}

// Playing reports a queue that owns the speaker's audio, even while paused.
func (s State) Playing() bool {
	return s.Status == "playing" || s.Status == "paused" || s.Status == "buffering"
}

// Sounding reports music the speaker is playing now; a paused queue is quiet.
func (s State) Sounding() bool {
	return s.Status == "playing" || s.Status == "buffering"
}

// PanelError is a command the player refused (HTTP 4xx with a message).
type PanelError struct {
	Status  int
	Message string
}

func (e *PanelError) Error() string {
	return fmt.Sprintf("ytm-alice refused (%d): %s", e.Status, e.Message)
}

type reply struct {
	State *State `json:"state"`
	Error string `json:"error"`
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 50 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (c *Client) origin() string {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// Command sends one panel command and returns the player state after it.
func (c *Client) Command(ctx context.Context, action, query string) (State, error) {
	return c.Send(ctx, Command{Action: action, Query: query})
}

// Command is a panel command. For play, Artist alone queues only that
// artist's songs, Title finds a catalog song by its title when Query finds
// nothing, and Spoken is searched last (the fork of ytm-alice).
type Command struct {
	Action string `json:"action"`
	Query  string `json:"query,omitempty"`
	Artist string `json:"artist,omitempty"`
	Title  string `json:"title,omitempty"`
	Spoken string `json:"spoken,omitempty"`
}

// Send posts a panel command and returns the new state.
func (c *Client) Send(ctx context.Context, command Command) (State, error) {
	body, err := json.Marshal(command)
	if err != nil {
		return State{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.origin()+"/api/command", bytes.NewReader(body))
	if err != nil {
		return State{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.origin())
	req.Header.Set("X-Ytm-Client", "web")
	return c.do(req)
}

// Current reads the player state without changing it.
func (c *Client) Current(ctx context.Context) (State, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.origin()+"/api/state", nil)
	if err != nil {
		return State{}, err
	}
	return c.do(req)
}

func (c *Client) do(req *http.Request) (State, error) {
	resp, err := c.client().Do(req)
	if err != nil {
		return State{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return State{}, err
	}
	var r reply
	if err := json.Unmarshal(data, &r); err != nil {
		return State{}, fmt.Errorf("ytm-alice reply: %w", err)
	}
	if resp.StatusCode >= 400 {
		if r.Error == "" {
			r.Error = http.StatusText(resp.StatusCode)
		}
		return State{}, &PanelError{Status: resp.StatusCode, Message: r.Error}
	}
	if r.State == nil {
		return State{}, errors.New("ytm-alice reply has no state")
	}
	return *r.State, nil
}
