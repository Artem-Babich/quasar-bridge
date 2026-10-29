package main

// User story: the owner turns on the Decisions route classifier with
// classifier_api "decisions". The bridge refuses to start unless the
// Decisions URL, model and QUASAR_CLASSIFIER_API_KEY are all set, so a
// misconfiguration is caught by -check-config instead of silently routing
// with Claude. Without classifier_api the bridge behaves as before, and
// leftover URL/model values are ignored so turning Decisions off is a
// single-field change.

import (
	"strings"
	"testing"
)

const decisionsConfig = `{"classifier_api":"decisions","classifier_base_url":"https://api.openai.example/v1","classifier_model":"gpt-6-luna"}`

func TestDecisionsWithoutKeyIsRejected(t *testing.T) {
	t.Setenv("QUASAR_CLASSIFIER_API_KEY", "")
	if _, err := readConfig(configFile(t, decisionsConfig)); err == nil || !strings.Contains(err.Error(), "QUASAR_CLASSIFIER_API_KEY") {
		t.Fatalf("missing key error=%v", err)
	}
	t.Setenv("QUASAR_CLASSIFIER_API_KEY", "fixture-key")
	cfg, err := readConfig(configFile(t, decisionsConfig))
	if err != nil || cfg.ClassifierAPI != "decisions" || cfg.ClassifierModel != "gpt-6-luna" {
		t.Fatalf("cfg=%+v err=%v", cfg.ClassifierAPI, err)
	}
}

func TestDecisionsRequiresBaseURLAndModel(t *testing.T) {
	t.Setenv("QUASAR_CLASSIFIER_API_KEY", "fixture-key")
	for _, body := range []string{
		`{"classifier_api":"decisions","classifier_model":"gpt-6-luna"}`,
		`{"classifier_api":"decisions","classifier_base_url":"ftp://api.openai.example","classifier_model":"gpt-6-luna"}`,
		`{"classifier_api":"decisions","classifier_base_url":"https://api.openai.example/v1"}`,
	} {
		if _, err := readConfig(configFile(t, body)); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestUnknownClassifierApiIsRejected(t *testing.T) {
	if _, err := readConfig(configFile(t, `{"classifier_api":"openai"}`)); err == nil {
		t.Fatal("unknown classifier_api accepted")
	}
}

func TestLlmIgnoresClassifierURLAndModel(t *testing.T) {
	t.Setenv("QUASAR_CLASSIFIER_API_KEY", "")
	if _, err := readConfig(configFile(t, `{"classifier_api":"llm","classifier_base_url":"not a url","classifier_model":""}`)); err != nil {
		t.Fatalf("llm classifier rejected leftovers: %v", err)
	}
}

func TestMissingClassifierApiMeansLlm(t *testing.T) {
	cfg, err := readConfig(configFile(t, `{}`))
	if err != nil || cfg.ClassifierAPI != "llm" {
		t.Fatalf("classifier_api=%q err=%v", cfg.ClassifierAPI, err)
	}
}
