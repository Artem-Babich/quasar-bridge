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
		if c := cfg.Routing.Classifier; c.DecisionsTimeoutMS >= c.TimeoutMS {
			return errors.New("classifier decisions_timeout_ms must be shorter than timeout_ms, or 0 for 1500 ms")
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
