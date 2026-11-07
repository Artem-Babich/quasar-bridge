package home

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMCPReadsSSEStructuredResultAndRejectsToolErrors(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.UnmarshalRead(r.Body, &body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if body.Method == "initialize" {
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"protocolVersion\":\"2025-03-26\"}}\n\n", body.ID)
			return
		}
		if body.Params.Name == "ha_get_state" && body.Params.Arguments["entity_id"] == "light.office" {
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"structuredContent\":{\"data\":{\"entity_id\":\"light.office\",\"state\":\"on\"}},\"isError\":false}}\n\n", body.ID)
		} else {
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"isError\":true,\"content\":[{\"type\":\"text\",\"text\":\"service failed\"}]}}\n\n", body.ID)
		}
	}))
	defer s.Close()
	c := MCP{URL: s.URL}
	got, err := c.Call(t.Context(), "ha_get_state", map[string]any{"entity_id": "light.office"})
	if err != nil || string(got) != `{"data":{"entity_id":"light.office","state":"on"}}` {
		t.Fatalf("result=%s err=%v", got, err)
	}
	if _, err := c.Call(t.Context(), "unknown_tool", nil); err == nil {
		t.Fatal("HTTP 200 tool error treated as success")
	}
}

func TestMCPAllowsConcurrentIndependentReads(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		_ = json.UnmarshalRead(r.Body, &b)
		if b.Method != "initialize" {
			arrived <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"structuredContent":{"success":true}}}`, b.ID)
	}))
	defer s.Close()
	c := MCP{URL: s.URL}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { _, _ = c.Call(ctx, "ha_get_state", nil) })
	}
	for range 2 {
		select {
		case <-arrived:
		case <-ctx.Done():
			close(release)
			wg.Wait()
			t.Fatal("independent reads serialized")
		}
	}
	close(release)
	wg.Wait()
}

// User story: ha-mcp hangs while the bridge connects to it for the first time.
// A list command that arrives meanwhile has only a few seconds. It gives up in
// time instead of waiting for the connection another command started, so the
// owner hears the failure. Once ha-mcp answers, the next command works.
func TestACommandDoesNotOutwaitItsTimeLimitBehindAHungFirstConnection(t *testing.T) {
	hung := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var connected atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		_ = json.UnmarshalRead(r.Body, &req)
		if req.Method == "initialize" {
			first := false
			once.Do(func() { first = true })
			if first {
				close(hung)
				select {
				case <-release:
				case <-r.Context().Done():
				}
				return
			}
			connected.Store(true)
			fmt.Fprintf(w, `{"id":%d,"result":{}}`, req.ID)
			return
		}
		if !connected.Load() {
			http.Error(w, "not initialized", http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, `{"id":%d,"result":{"structuredContent":{"success":true}}}`, req.ID)
	}))
	defer srv.Close()
	defer close(release)
	c := &MCP{URL: srv.URL}

	firstDone := make(chan struct{})
	firstCtx, stopFirst := context.WithCancel(t.Context())
	go func() {
		defer close(firstDone)
		_, _ = c.Call(firstCtx, "first", nil)
	}()
	<-hung

	limited, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := c.Call(limited, "second", nil)
		finished <- err
	}()
	select {
	case err := <-finished:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the command outwaited its time limit")
	}

	stopFirst()
	<-firstDone
	if _, err := c.Call(t.Context(), "third", nil); err != nil {
		t.Fatalf("next command after the hang: %v", err)
	}
}
