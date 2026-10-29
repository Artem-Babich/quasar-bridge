package main

// User story: the decisions_timeout_ms check follows the classifier that is
// really active, including one switched by runtime.env, and the probe line
// says plainly when it cannot tell whether a connection was reused.

import (
	"testing"
)

func TestDecisionsTimeoutIsCheckedWhenRuntimeEnvEnablesDecisions(t *testing.T) {
	t.Setenv("QUASAR_CLASSIFIER_API_KEY", "fixture-key")
	t.Setenv("QUASAR_CLASSIFIER_API", "decisions")
	t.Setenv("QUASAR_CLASSIFIER_BASE_URL", "https://api.openai.example/v1")
	t.Setenv("QUASAR_CLASSIFIER_MODEL", "gpt-6-luna")
	if _, err := readConfig(configFile(t, `{"classifier_api":"llm",`+timeoutConfig("1000", "5000"))); err == nil {
		t.Fatal("runtime.env enabled Decisions with decisions_timeout_ms above timeout_ms and the config was accepted")
	}
}

func TestDecisionsTimeoutIsNotCheckedWhenRuntimeEnvDisablesDecisions(t *testing.T) {
	t.Setenv("QUASAR_CLASSIFIER_API", "")
	if _, err := readConfig(configFile(t, decisionsHead+timeoutConfig("1000", "5000"))); err != nil {
		t.Fatalf("runtime.env turned Decisions off but the timeout was still checked: %v", err)
	}
}

func TestProbeConnectionLabels(t *testing.T) {
	yes, no := true, false
	for reused, want := range map[*bool]string{nil: "connection unknown", &yes: "reused connection", &no: "new connection"} {
		if got := connection(reused); got != want {
			t.Fatalf("connection(%v)=%q, want %q", reused, got, want)
		}
	}
}
