package main

// User story: after raising decisions_timeout_ms and running systemctl reload,
// the owner sees a new classifier_backend line in the journal with the new
// Decisions timeout, so they know the change took effect.

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"

	"quasarbridge/internal/bridge"
)

func TestReloadLogsNewDecisionsTimeout(t *testing.T) {
	t.Setenv("QUASAR_CLASSIFIER_API_KEY", "fixture-key")
	path := configFile(t, decisionsHead+timeoutConfig("8000", "300"))
	initial, err := readConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(decisionsHead+timeoutConfig("8000", "900")), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&out, nil)))
	defer slog.SetDefault(previous)
	if err := reloadRouting(path, initial, new(bridge.Engine)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"msg":"classifier_backend","backend":"decisions","model":"gpt-6-luna","decisions_timeout_ms":900`) {
		t.Fatalf("reload log:\n%s", out.String())
	}
}
