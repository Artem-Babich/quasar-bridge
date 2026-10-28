package main

import (
	"errors"
	"os"
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
	default:
		return errors.New("classifier_api must be llm or decisions")
	}
	return nil
}
