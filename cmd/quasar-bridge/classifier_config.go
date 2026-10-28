package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"quasarbridge/internal/llm"
)

// validateClassifier checks the fork's classifier settings. With "llm" the
// Decisions URL and model are ignored, so switching back is one field.
func validateClassifier(cfg *config) error {
	applyClassifierEnv(cfg)
	switch cfg.ClassifierAPI {
	case "":
		cfg.ClassifierAPI = "llm"
	case "llm":
	case "decisions":
		if !validEndpoint(cfg.ClassifierBaseURL, "http", "https") {
			return errors.New("classifier_api decisions requires a valid classifier_base_url")
		}
		if cfg.ClassifierModel == "" {
			return errors.New("classifier_api decisions requires classifier_model")
		}
		if os.Getenv("QUASAR_CLASSIFIER_API_KEY") == "" {
			return errors.New("classifier_api decisions requires QUASAR_CLASSIFIER_API_KEY")
		}
		c := cfg.Routing.Classifier
		decisionsMS := c.DecisionsTimeoutMS
		if decisionsMS == 0 {
			decisionsMS = 1500
		}
		if decisionsMS >= c.TimeoutMS {
			return fmt.Errorf("classifier decisions_timeout_ms (%d ms, 0 means 1500) must be shorter than timeout_ms (%d ms)", decisionsMS, c.TimeoutMS)
		}
	default:
		return errors.New("classifier_api must be llm or decisions")
	}
	return nil
}

// checkClassifier probes Decisions for -check-backends. It returns the text
// appended to the check line, or nothing when Decisions is off.
func checkClassifier(ctx context.Context, c *llm.DecisionsClient, prompt string) (string, error) {
	if c == nil {
		return "", nil
	}
	start := time.Now()
	_, err := c.ChooseRoute(ctx, prompt, []llm.Message{{Role: "user", Content: "Ответь одним словом: связь"}}, []string{"home", "llm", "yandex"})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("; Classifier: decisions ok (%d ms)", time.Since(start).Milliseconds()), nil
}

// checkBackendsLine is the -check-backends summary line.
func checkBackendsLine(homeEnabled bool, entities int, classifierStatus string) string {
	if !homeEnabled {
		return "LLM: ok; Home Assistant: disabled" + classifierStatus
	}
	return fmt.Sprintf("LLM: ok; Home Assistant: %d entities%s", entities, classifierStatus)
}

// applyClassifierEnv lets runtime.env switch the classifier without changing
// config.json. A variable that is set wins even when empty.
func applyClassifierEnv(cfg *config) {
	for name, target := range map[string]*string{
		"QUASAR_CLASSIFIER_API":      &cfg.ClassifierAPI,
		"QUASAR_CLASSIFIER_BASE_URL": &cfg.ClassifierBaseURL,
		"QUASAR_CLASSIFIER_MODEL":    &cfg.ClassifierModel,
	} {
		if value, ok := os.LookupEnv(name); ok {
			*target = value
		}
	}
}
