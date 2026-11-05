package llm

// User story: after a pause the first route choice is slow because the
// connection to OpenAI has gone cold. While the bridge runs, a small free
// request to the same host keeps the connection open, so the next choice
// reuses it. The keeper never calls /decisions, so it spends no Decisions
// calls, and it stops with the bridge. A failing keeper never breaks routing.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func warmClient(t *testing.T, handler http.HandlerFunc, idle time.Duration) (*DecisionsClient, func()) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.IdleConnTimeout = idle
	t.Cleanup(transport.CloseIdleConnections)
	hc := &http.Client{Transport: transport}
	return &DecisionsClient{BaseURL: server.URL + "/v1", Model: "route/model", APIKey: "key", HTTP: hc}, transport.CloseIdleConnections
}

func TestKeepWarmAsksForTheModelAndNeverCallsDecisions(t *testing.T) {
	var pings, decisions atomic.Int32
	var bad atomic.Value
	c, _ := warmClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/decisions") {
			decisions.Add(1)
			return
		}
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/v1/models/route%2Fmodel" || r.Header.Get("Authorization") != "Bearer key" {
			bad.Store(r.Method + " " + r.URL.EscapedPath())
		}
		pings.Add(1)
		w.Write([]byte(`{"id":"route/model"}`))
	}, time.Minute)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.KeepWarm(ctx, 5*time.Millisecond)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for pings.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("keeper did not stop with its context")
	}
	if pings.Load() < 3 || decisions.Load() != 0 || bad.Load() != nil {
		t.Fatalf("pings=%d decisions=%d bad=%v", pings.Load(), decisions.Load(), bad.Load())
	}
}

func TestKeepWarmHoldsTheConnectionOpenForTheNextRouteChoice(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/decisions") {
			w.Write([]byte(`{"answers":[{"type":"choice","choice":"llm","confidence":0.9}]}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"code":"invalid_api_key"}}`))
	}
	const idle = 300 * time.Millisecond
	choose := func(c *DecisionsClient) bool {
		t.Helper()
		answer, err := c.ChooseRoute(t.Context(), "выбери", []Message{{Role: "user", Content: "фраза"}}, []string{"llm"})
		if err != nil || answer.ConnReused == nil {
			t.Fatalf("answer=%+v err=%v", answer, err)
		}
		return *answer.ConnReused
	}
	cold, _ := warmClient(t, handler, idle)
	choose(cold)
	time.Sleep(3 * idle)
	if choose(cold) {
		t.Fatal("control connection stayed open without a keeper; the test proves nothing")
	}
	warm, _ := warmClient(t, handler, idle)
	go warm.KeepWarm(t.Context(), idle/6)
	time.Sleep(3 * idle)
	if !choose(warm) {
		t.Fatal("route choice opened a new connection although the keeper ran")
	}
}

func TestKeepWarmReopensAConnectionThatWasLost(t *testing.T) {
	var pings atomic.Int32
	c, drop := warmClient(t, func(w http.ResponseWriter, r *http.Request) {
		pings.Add(1)
	}, time.Minute)
	go c.KeepWarm(t.Context(), 5*time.Millisecond)
	drop()
	deadline := time.Now().Add(2 * time.Second)
	for pings.Load() < 3 && time.Now().Before(deadline) {
		drop()
		time.Sleep(10 * time.Millisecond)
	}
	if pings.Load() < 3 {
		t.Fatalf("keeper stopped after a lost connection: pings=%d", pings.Load())
	}
}

// failingTransport fails every request and counts how often the idle
// connections were closed.
type failingTransport struct {
	closed atomic.Int32
}

func (f *failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection lost")
}

func (f *failingTransport) CloseIdleConnections() { f.closed.Add(1) }

// User story: the network drops and the pooled connection stays dead. A failed
// keep-warm request drops the idle connections, so the next route choice opens
// a new one instead of stalling on the dead one until its timeout.
func TestKeepWarmDropsIdleConnectionsAfterAFailedRequest(t *testing.T) {
	transport := &failingTransport{}
	c := &DecisionsClient{BaseURL: "https://example.invalid/v1", Model: "route/model", APIKey: "key", HTTP: &http.Client{Transport: transport}}
	if status := c.ping(t.Context()); status != 0 {
		t.Fatalf("status = %d", status)
	}
	if transport.closed.Load() != 1 {
		t.Fatalf("idle connections closed %d times", transport.closed.Load())
	}
}

// User story: any HTTP answer proves the connection is alive, even an error
// status, so the keeper leaves it in the pool.
func TestKeepWarmKeepsTheConnectionAfterAnErrorStatus(t *testing.T) {
	var closed atomic.Int32
	c, _ := warmClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}, time.Minute)
	c.HTTP.Transport = closeCounter{RoundTripper: c.HTTP.Transport, closed: &closed}
	if status := c.ping(t.Context()); status != http.StatusUnauthorized {
		t.Fatalf("status = %d", status)
	}
	if closed.Load() != 0 {
		t.Fatalf("idle connections closed %d times", closed.Load())
	}
}

type closeCounter struct {
	http.RoundTripper
	closed *atomic.Int32
}

func (c closeCounter) CloseIdleConnections() { c.closed.Add(1) }
