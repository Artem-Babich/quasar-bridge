package main

// User story: when a Decisions setting comes from runtime.env, a config
// error names the environment variable too, so the owner does not search
// config.json for a value that is not there. With Decisions off, the
// startup log says the LLM classifier is active.

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestClassifierConfigErrorsNameTheEnvironmentVariable(t *testing.T) {
	t.Setenv("QUASAR_CLASSIFIER_API_KEY", "fixture-key")
	for env, value := range map[string]string{
		"QUASAR_CLASSIFIER_API":      "Decisions",
		"QUASAR_CLASSIFIER_BASE_URL": "not a url",
		"QUASAR_CLASSIFIER_MODEL":    "",
	} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("QUASAR_CLASSIFIER_API", "decisions")
			t.Setenv("QUASAR_CLASSIFIER_BASE_URL", "https://api.openai.example/v1")
			t.Setenv("QUASAR_CLASSIFIER_MODEL", "gpt-6-luna")
			t.Setenv(env, value)
			_, err := readConfig(configFile(t, `{}`))
			if err == nil || !strings.Contains(err.Error(), env) {
				t.Fatalf("error %v does not name %s", err, env)
			}
		})
	}
}

func TestStartupLogNamesLlmClassifierByDefault(t *testing.T) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	body, err := json.Marshal(map[string]any{"mode": "hybrid", "listen": address, "llm_base_url": "https://llm.example.invalid/v1", "llm_model": "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestClassifierServerChild$")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "QUASAR_") {
			child.Env = append(child.Env, entry)
		}
	}
	child.Env = append(child.Env, "QUASAR_CLASSIFIER_SERVER_CHILD=1", "QUASAR_CLASSIFIER_CONFIG="+configFile(t, string(body)))
	var out bytes.Buffer
	child.Stdout, child.Stderr = &out, &out
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	hc := &http.Client{Timeout: time.Second}
	for {
		if r, err := hc.Get("http://" + address + "/healthz"); err == nil {
			r.Body.Close()
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("server did not start:\n%s", out.String())
		case <-time.After(10 * time.Millisecond):
		}
	}
	_ = child.Process.Signal(syscall.SIGTERM)
	_ = child.Wait()
	if !strings.Contains(out.String(), `"msg":"classifier_backend","backend":"llm"`) {
		t.Fatalf("startup log lacks llm classifier line:\n%s", out.String())
	}
}
