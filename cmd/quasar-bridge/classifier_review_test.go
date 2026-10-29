package main

// User story: the owner's speaker host runs with Home Assistant, so the
// Decisions status must also appear on the -check-backends line that counts
// HA entities. decisions_timeout_ms is read from routing.classifier and can be
// changed with SIGHUP, while switching the Decisions model needs a restart.
// A failed Decisions check exits with code 1 and says what failed.

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"quasarbridge/internal/bridge"
)

func TestCheckBackendsLineCarriesClassifierStatusWithHomeAssistant(t *testing.T) {
	const status = "; Classifier: decisions ok, route llm"
	if got := checkBackendsLine(true, 63, status); got != "LLM: ok; Home Assistant: 63 entities"+status {
		t.Fatalf("line %q", got)
	}
	if got := checkBackendsLine(false, 0, status); got != "LLM: ok; Home Assistant: disabled"+status {
		t.Fatalf("line %q", got)
	}
}

func TestDecisionsTimeoutIsReadFromRoutingJSON(t *testing.T) {
	cfg, err := readConfig(configFile(t, `{"routing":{"classifier":{"timeout_ms":8000,"on_error":"yandex","prompt":"p","decisions_timeout_ms":300}}}`))
	if err != nil || cfg.Routing.Classifier.DecisionsTimeoutMS != 300 {
		t.Fatalf("decisions_timeout_ms=%d err=%v", cfg.Routing.Classifier.DecisionsTimeoutMS, err)
	}
}

func TestReloadAppliesDecisionsTimeoutButNotClassifierModel(t *testing.T) {
	t.Setenv("QUASAR_CLASSIFIER_API_KEY", "fixture-key")
	const head = `{"classifier_api":"decisions","classifier_base_url":"https://api.openai.example/v1",`
	path := configFile(t, head+`"classifier_model":"m1","routing":{"classifier":{"timeout_ms":8000,"on_error":"yandex","prompt":"p"}}}`)
	initial, err := readConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	engine := new(bridge.Engine)
	if err := engine.SetRouting(initial.Routing); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(head+`"classifier_model":"m1","routing":{"classifier":{"timeout_ms":8000,"on_error":"yandex","prompt":"p","decisions_timeout_ms":900}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	before := engine.RoutingStatus().Revision
	if err := reloadRouting(path, initial, engine); err != nil || engine.RoutingStatus().Revision == before {
		t.Fatalf("decisions_timeout_ms not reloaded: %v", err)
	}
	if err := os.WriteFile(path, []byte(head+`"classifier_model":"m2","routing":{"classifier":{"timeout_ms":8000,"on_error":"yandex","prompt":"p"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := reloadRouting(path, initial, engine); err == nil || !strings.Contains(err.Error(), "restart required") {
		t.Fatalf("classifier_model change accepted on reload: %v", err)
	}
}

func TestCheckBackendsFailureExitsOneAndNamesTheStatus(t *testing.T) {
	out, err := runClassifierChild(t, "check-backends", http.StatusUnauthorized)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(out, "Decisions classifier check failed") || !strings.Contains(out, "Decisions http 401") {
		t.Fatalf("err=%v output:\n%s", err, out)
	}
}
