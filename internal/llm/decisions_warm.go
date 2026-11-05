package llm

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	decisionsWarmTimeout = 10 * time.Second
	maxWarmResponseBytes = 64 << 10
)

// KeepWarm holds the connection to the Decisions host open until ctx ends. It
// sends GET /models/{model} at once and then every interval, over the client
// ChooseRoute uses. That request is not a Decisions call and costs no tokens.
// Any HTTP status keeps the connection, so only a change of state is logged.
func (c *DecisionsClient) KeepWarm(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	last := -1
	for {
		if status := c.ping(ctx); status != last && ctx.Err() == nil {
			slog.Info("decisions_keep_warm", "status", status)
			last = status
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ping returns the HTTP status, or 0 when no response arrived.
func (c *DecisionsClient) ping(ctx context.Context) int {
	ctx, cancel := context.WithTimeout(ctx, decisionsWarmTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+"/models/"+url.PathEscape(c.Model), nil)
	if err != nil {
		return 0
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		// Pooled HTTP/2 connections are shared: a dead one would stall the next
		// route choice too, so the next request has to dial again.
		hc.CloseIdleConnections()
		return 0
	}
	defer resp.Body.Close()
	// Reading the body to the end is what returns the connection to the pool.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxWarmResponseBytes))
	return resp.StatusCode
}
