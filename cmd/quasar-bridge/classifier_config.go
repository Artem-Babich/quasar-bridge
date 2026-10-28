package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"quasarbridge/internal/bridge"
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

// checkClassifier probes Decisions for -check-backends with a cold and a warm
// call. It returns the text appended to the check line, or nothing when
// Decisions is off.
func checkClassifier(ctx context.Context, c *llm.DecisionsClient, classifier bridge.ClassifierConfig) (string, error) {
	if c == nil {
		return "", nil
	}
	probe := []llm.Message{{Role: "user", Content: "Ответь одним словом: связь"}}
	routes := []string{"home", "llm", "yandex"}
	start := time.Now()
	answer, err := c.ChooseRoute(ctx, classifier.Prompt, probe, routes)
	if err != nil {
		return "", err
	}
	cold := time.Since(start)
	start = time.Now()
	if _, err := c.ChooseRoute(ctx, classifier.Prompt, probe, routes); err != nil {
		return "", err
	}
	warm := time.Since(start)
	timeout := classifier.DecisionsTimeout()
	status := fmt.Sprintf("; Classifier: decisions ok, route %s, confidence %.2f, cold %d ms, warm %d ms (timeout %d ms)",
		answer.Route, answer.Confidence, cold.Milliseconds(), warm.Milliseconds(), timeout.Milliseconds())
	if cold > timeout {
		status += "; WARNING: cold call exceeds decisions_timeout_ms"
	}
	return status, nil
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

// logClassifierBackend names the active route classifier at startup. It never
// logs the API key.
func logClassifierBackend(cfg config) {
	if cfg.ClassifierAPI != "decisions" {
		slog.Info("classifier_backend", "backend", "llm")
		return
	}
	slog.Info("classifier_backend", "backend", "decisions", "model", cfg.ClassifierModel,
		"decisions_timeout_ms", cfg.Routing.Classifier.DecisionsTimeout().Milliseconds())
}
