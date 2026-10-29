package bridge

// User story: a real speaker request routed by a classifier writes the
// native_route_decision log line with the classifier backend and latency, so
// the owner can compare classifiers from journalctl.

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	common "github.com/freequasar/quasar-go-genproto/proto/alice/megamind/protos/common"
	request "github.com/freequasar/quasar-go-genproto/proto/alice/protos/api/alicekit/events/alice_request"
	header "github.com/freequasar/quasar-go-genproto/proto/alice/protos/api/alicekit/events/common/event_header"
	client "github.com/freequasar/quasar-go-genproto/proto/alice/protos/api/alicekit/protocol/client"

	"quasarbridge/internal/llm"
	"quasarbridge/internal/wire"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestNativeRequestLogsClassifierBackend(t *testing.T) {
	logs := new(lockedBuffer)
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	up := make(chan []byte, 16)
	down := make(chan []byte, 16)
	engine := &Engine{LLM: completionFunc(func(_ context.Context, _ []llm.Message, structured bool) (string, error) {
		if structured {
			return `{"route":"llm"}`, nil
		}
		return "Ответ", nil
	})}
	cfg := DefaultRoutingConfig()
	cfg.Rules = []RoutingRule{}
	if err := engine.SetRouting(cfg); err != nil {
		t.Fatal(err)
	}
	n := NewNative(ctx, func(_ context.Context, _ websocket.MessageType, b []byte) error { up <- b; return nil }, func(_ context.Context, _ websocket.MessageType, b []byte) error { down <- b; return nil }, engine)
	defer n.Close()
	in := aa(&client.TClientMessage{Message: &client.TClientMessage_Event{Event: &client.TClientMessage_TEvent{Header: &header.TEventHeader{MessageId: "log-check"}, Event: &client.TClientMessage_TEvent_TextInput{TextInput: &request.TAliceRequestProto{Request: &request.TAliceRequestProto_TRequest{Event: &common.TEvent{Text: new("сочини стих про кота")}}}}}}})
	if err := n.Client(ctx, websocket.MessageBinary, in); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case <-up:
		case b := <-down:
			if wire.Decode(b, wire.ServerToClient).Kind() != "AliceResponse" {
				continue
			}
			for line := range strings.SplitSeq(logs.String(), "\n") {
				if strings.Contains(line, `"msg":"native_route_decision"`) {
					if !strings.Contains(line, `"backend":"llm"`) || !strings.Contains(line, `"latency_ms":`) {
						t.Fatalf("decision log lacks classifier fields: %s", line)
					}
					return
				}
			}
			t.Fatalf("no native_route_decision line:\n%s", logs.String())
		case <-ctx.Done():
			t.Fatal("no local answer")
		}
	}
}
