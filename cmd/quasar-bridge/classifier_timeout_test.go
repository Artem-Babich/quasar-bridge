package main

// User story: with the Decisions classifier on, decisions_timeout_ms must be
// shorter than timeout_ms; otherwise a slow Decisions answer would use up the
// whole classifier step and Claude would never get time to decide. The bridge
// refuses such a config at start (-check-config) and on reload, so the owner
// sees the mistake instead of silent on_error routing. 0 counts as its real
// value, 1500 ms. With Decisions off the field does nothing and is not checked.

import (
	"os"
	"strings"
	"testing"

	"quasarbridge/internal/bridge"
)

const decisionsHead = `{"classifier_api":"decisions","classifier_base_url":"https://api.openai.example/v1","classifier_model":"gpt-6-luna",`

func timeoutConfig(timeoutMS, decisionsMS string) string {
	return `"routing":{"classifier":{"timeout_ms":` + timeoutMS + `,"on_error":"yandex","prompt":"p","decisions_timeout_ms":` + decisionsMS + `}}}`
}

func TestDecisionsTimeoutNotShorterThanTimeoutIsRejected(t *testing.T) {
	t.Setenv("QUASAR_CLASSIFIER_API_KEY", "fixture-key")
	for _, decisionsMS := range []string{"0", "1000", "5000"} {
		_, err := readConfig(configFile(t, decisionsHead+timeoutConfig("1000", decisionsMS)))
		if err == nil || !strings.Contains(err.Error(), "decisions_timeout_ms") || !strings.Contains(err.Error(), "timeout_ms") {
			t.Fatalf("decisions_timeout_ms %s with timeout_ms 1000: err=%v", decisionsMS, err)
		}
	}
	for _, decisionsMS := range []string{"999"} {
		if _, err := readConfig(configFile(t, decisionsHead+timeoutConfig("1000", decisionsMS))); err != nil {
			t.Fatalf("decisions_timeout_ms %s with timeout_ms 1000 rejected: %v", decisionsMS, err)
		}
	}
}

func TestDecisionsTimeoutIsNotCheckedWhenDecisionsIsOff(t *testing.T) {
	if _, err := readConfig(configFile(t, `{`+timeoutConfig("1000", "5000"))); err != nil {
		t.Fatalf("llm classifier rejected unused decisions_timeout_ms: %v", err)
	}
}

func TestReloadRejectsDecisionsTimeoutNotShorterThanTimeout(t *testing.T) {
	t.Setenv("QUASAR_CLASSIFIER_API_KEY", "fixture-key")
	path := configFile(t, decisionsHead+timeoutConfig("8000", "300"))
	initial, err := readConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	engine := new(bridge.Engine)
	if err := engine.SetRouting(initial.Routing); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(decisionsHead+timeoutConfig("8000", "8000")), 0600); err != nil {
		t.Fatal(err)
	}
	before := engine.RoutingStatus().Revision
	if err := reloadRouting(path, initial, engine); err == nil || engine.RoutingStatus().Revision != before {
		t.Fatalf("reload accepted decisions_timeout_ms equal to timeout_ms: %v", err)
	}
}

func TestZeroDecisionsTimeoutCountsAs1500(t *testing.T) {
	t.Setenv("QUASAR_CLASSIFIER_API_KEY", "fixture-key")
	if _, err := readConfig(configFile(t, decisionsHead+timeoutConfig("1500", "0"))); err == nil || !strings.Contains(err.Error(), "1500") {
		t.Fatalf("decisions_timeout_ms 0 with timeout_ms 1500: err=%v", err)
	}
	if _, err := readConfig(configFile(t, decisionsHead+timeoutConfig("1501", "0"))); err != nil {
		t.Fatalf("decisions_timeout_ms 0 with timeout_ms 1501 rejected: %v", err)
	}
}

func TestReloadRejectionNamesDecisionsTimeout(t *testing.T) {
	t.Setenv("QUASAR_CLASSIFIER_API_KEY", "fixture-key")
	path := configFile(t, decisionsHead+timeoutConfig("8000", "300"))
	initial, err := readConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(decisionsHead+timeoutConfig("8000", "9000")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := reloadRouting(path, initial, new(bridge.Engine)); err == nil || !strings.Contains(err.Error(), "decisions_timeout_ms") {
		t.Fatalf("reload error=%v", err)
	}
}
