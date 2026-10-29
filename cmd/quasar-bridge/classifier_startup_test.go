package main

// User story: when the bridge starts, the log says which route classifier is
// active (and for Decisions, which model and timeout), so after editing
// runtime.env the owner can confirm the switch from journalctl. The API key
// never appears in the log.

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"flag"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestClassifierServerChild(t *testing.T) {
	if os.Getenv("QUASAR_CLASSIFIER_SERVER_CHILD") == "" {
		t.Skip("child process helper")
	}
	flag.CommandLine = flag.NewFlagSet("quasar-bridge", flag.ExitOnError)
	os.Args = []string{"quasar-bridge", "-config", os.Getenv("QUASAR_CLASSIFIER_CONFIG")}
	main()
}

func TestStartupLogNamesClassifierBackendWithoutKey(t *testing.T) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	body, err := json.Marshal(map[string]any{"mode": "hybrid", "listen": address, "llm_base_url": "https://llm.example.invalid/v1", "llm_model": "fixture",
		"classifier_api": "decisions", "classifier_base_url": "https://decisions.example.invalid/v1", "classifier_model": "fixture-decisions"})
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
	child.Env = append(child.Env, "QUASAR_CLASSIFIER_SERVER_CHILD=1", "QUASAR_CLASSIFIER_CONFIG="+configFile(t, string(body)), "QUASAR_CLASSIFIER_API_KEY=sk-fixture-secret")
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
	log := out.String()
	if !strings.Contains(log, `"msg":"classifier_backend","backend":"decisions","model":"fixture-decisions","decisions_timeout_ms":1500`) {
		t.Fatalf("startup log lacks classifier backend:\n%s", log)
	}
	if strings.Contains(log, "sk-fixture-secret") {
		t.Fatalf("startup log leaks the key:\n%s", log)
	}
}
