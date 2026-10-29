package main

import (
	"os"
	"testing"
)

// TestMain keeps the operator's own QUASAR_CLASSIFIER_* variables (for
// example an exported runtime.env) from changing what the tests read.
// Child processes started by the process tests get their variables
// explicitly and keep them.
func TestMain(m *testing.M) {
	if os.Getenv("QUASAR_CLASSIFIER_CHILD") == "" && os.Getenv("QUASAR_CLASSIFIER_SERVER_CHILD") == "" {
		for _, name := range []string{"QUASAR_CLASSIFIER_API", "QUASAR_CLASSIFIER_BASE_URL", "QUASAR_CLASSIFIER_MODEL", "QUASAR_CLASSIFIER_API_KEY"} {
			os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}
