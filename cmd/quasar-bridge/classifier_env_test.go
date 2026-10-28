package main

// User story: the owner enables Decisions on the speaker host by editing only
// runtime.env, so config.json stays readable by the previous release and a
// rollback is a binary swap. Setting QUASAR_CLASSIFIER_API to an empty value
// turns Decisions off even if config.json enables it.

import "testing"

func TestClassifierEnvOverridesJSON(t *testing.T) {
	t.Setenv("QUASAR_CLASSIFIER_API", "decisions")
	t.Setenv("QUASAR_CLASSIFIER_BASE_URL", "https://api.openai.example/v1")
	t.Setenv("QUASAR_CLASSIFIER_MODEL", "gpt-6-luna")
	t.Setenv("QUASAR_CLASSIFIER_API_KEY", "fixture-key")
	cfg, err := readConfig(configFile(t, `{"classifier_api":"llm","classifier_model":"json-model"}`))
	if err != nil || cfg.ClassifierAPI != "decisions" || cfg.ClassifierBaseURL != "https://api.openai.example/v1" || cfg.ClassifierModel != "gpt-6-luna" {
		t.Fatalf("api=%q url=%q model=%q err=%v", cfg.ClassifierAPI, cfg.ClassifierBaseURL, cfg.ClassifierModel, err)
	}
}

func TestEmptyClassifierEnvStillWins(t *testing.T) {
	t.Setenv("QUASAR_CLASSIFIER_API", "")
	t.Setenv("QUASAR_CLASSIFIER_API_KEY", "fixture-key")
	cfg, err := readConfig(configFile(t, decisionsConfig))
	if err != nil || cfg.ClassifierAPI != "llm" {
		t.Fatalf("api=%q err=%v", cfg.ClassifierAPI, err)
	}
}
