package bridge

// User story: «Заведи таймер» goes to Yandex, Yandex asks «На какое время
// поставить таймер?», and the owner answers «На 30 секунд». With
// routing.classifier.route_context on, Decisions and its Claude fallback see
// that exchange with the reply tagged [yandex], so they send the answer back
// to whoever asked instead of to Claude. They see the last 3 turns of all
// routes from the last 2 minutes. The answer model still sees only its own
// dialogue, private history still never leaves for OpenAI, and «начни новый
// разговор» forgets the record too. With route_context off nothing changes.

import (
	"context"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	app "github.com/freequasar/quasar-go-genproto/proto/alice/library/client/protos"
	common "github.com/freequasar/quasar-go-genproto/proto/alice/megamind/protos/common"
	request "github.com/freequasar/quasar-go-genproto/proto/alice/protos/api/alicekit/events/alice_request"
	response "github.com/freequasar/quasar-go-genproto/proto/alice/protos/api/alicekit/events/alice_response"
	header "github.com/freequasar/quasar-go-genproto/proto/alice/protos/api/alicekit/events/common/event_header"
	client "github.com/freequasar/quasar-go-genproto/proto/alice/protos/api/alicekit/protocol/client"
	server "github.com/freequasar/quasar-go-genproto/proto/alice/protos/api/alicekit/protocol/server"
	deferred "github.com/freequasar/quasar-go-genproto/proto/alice/protos/api/alicekit/streaming"
	"quasarbridge/internal/dialogue"
	"quasarbridge/internal/llm"
	"quasarbridge/internal/wire"
)

func routeContextConfig() RoutingConfig {
	cfg := DefaultRoutingConfig()
	cfg.Rules = nil
	cfg.Classifier.RouteContext = true
	return cfg
}

func messageTexts(m []llm.Message) []string {
	var out []string
	for _, v := range m {
		out = append(out, v.Role+": "+v.Content)
	}
	return out
}

// speakerRig is one speaker connection: text requests go in, Yandex replies
// are played back, and frames sent to Yandex can be awaited.
type speakerRig struct {
	t    *testing.T
	e    *Engine
	n    *Native
	down chan wire.Frame
	up   chan wire.Frame
	seq  int
}

func newSpeakerRig(t *testing.T, e *Engine) *speakerRig {
	t.Helper()
	r := &speakerRig{t: t, e: e, down: make(chan wire.Frame, 64), up: make(chan wire.Frame, 64)}
	r.n = NewNative(t.Context(), func(_ context.Context, _ websocket.MessageType, b []byte) error {
		r.up <- wire.Decode(b, wire.ClientToServer)
		return nil
	}, func(_ context.Context, _ websocket.MessageType, b []byte) error {
		r.down <- wire.Decode(b, wire.ServerToClient)
		return nil
	}, e)
	t.Cleanup(r.n.Close)
	return r
}

func (r *speakerRig) say(text string) string {
	r.t.Helper()
	r.seq++
	id := "request-" + string(rune('a'+r.seq))
	input := &client.TClientMessage{Message: &client.TClientMessage_Event{Event: &client.TClientMessage_TEvent{Header: &header.TEventHeader{MessageId: id}, Event: &client.TClientMessage_TEvent_TextInput{TextInput: &request.TAliceRequestProto{Application: &app.TClientInfoProto{DeviceId: new("speaker")}, Request: &request.TAliceRequestProto_TRequest{Event: &common.TEvent{Text: new(text)}}}}}}}
	if err := r.n.Client(r.t.Context(), websocket.MessageBinary, aa(input)); err != nil {
		r.t.Fatal(err)
	}
	return id
}

// waitUp returns the first upstream frame of the kind.
func (r *speakerRig) waitUp(kinds ...string) wire.Frame {
	r.t.Helper()
	for {
		select {
		case f := <-r.up:
			if slices.Contains(kinds, f.Kind()) {
				return f
			}
		case <-time.After(2 * time.Second):
			r.t.Fatalf("no upstream %v", kinds)
		}
	}
}

// waitTurn blocks until the engine recorded a turn with this text.
func (r *speakerRig) waitTurn(want string) {
	r.t.Helper()
	ctx := dialogue.WithSpeaker(r.t.Context(), "device:speaker")
	for range 200 {
		for _, m := range r.e.dialogue().RouteContext(ctx).Messages {
			if m.Content == want {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.t.Fatalf("turn %q never recorded: %+v", want, r.e.dialogue().RouteContext(ctx).Messages)
}

func (r *speakerRig) yandexAnswers(id, speech string) {
	r.t.Helper()
	answer := &server.TServerMessage{Message: &server.TServerMessage_Event{Event: &server.TServerMessage_TEvent{Header: &header.TEventHeader{RefMessageId: id}, Event: &server.TServerMessage_TEvent_AliceResponse{AliceResponse: &response.TAliceResponseProto{VoiceResponse: &response.TAliceResponseProto_TVoiceResponse{OutputSpeech: &response.TAliceResponseProto_TVoiceResponse_TOutputSpeech{Text: new(speech)}}}}}}}
	if err := r.n.Server(r.t.Context(), websocket.MessageBinary, aa(answer)); err != nil {
		r.t.Fatal(err)
	}
}

func TestYandexQuestionReachesDecisionsTaggedSoTheAnswerGoesBackToYandex(t *testing.T) {
	seen := make(chan []llm.Message, 8)
	e := decisionsEngine(t, routeContextConfig(), func(_ context.Context, _ string, m []llm.Message, _ []string) (llm.RouteAnswer, error) {
		seen <- slices.Clone(m)
		return llm.RouteAnswer{Route: "yandex"}, nil
	}, failingClaude(t))
	r := newSpeakerRig(t, e)
	first := r.say("Заведи таймер")
	r.waitUp("TextInput")
	<-seen
	r.yandexAnswers(first, "На какое время поставить таймер?")
	r.waitTurn("[yandex] На какое время поставить таймер?")
	r.say("На 30 секунд")
	want := []string{"user: Заведи таймер", "assistant: [yandex] На какое время поставить таймер?", "user: На 30 секунд"}
	if got := messageTexts(<-seen); !slices.Equal(got, want) {
		t.Fatalf("Decisions saw %q, want %q", got, want)
	}
}

func TestClaudeClassifierFallbackGetsTheSameTaggedContext(t *testing.T) {
	var got []llm.Message
	e := decisionsEngine(t, routeContextConfig(), func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{}, &llm.DecisionsError{Class: "timeout"}
	}, func(_ context.Context, m []llm.Message, _ bool) (string, error) {
		got = slices.Clone(m)
		return `{"route":"yandex"}`, nil
	})
	ctx := e.Prepare(dialogue.WithSpeaker(t.Context(), "device:speaker"))
	e.RecordRouteTurn(ctx, "yandex", "Заведи таймер", "На какое время поставить таймер?", true)
	next := e.Prepare(dialogue.WithSpeaker(t.Context(), "device:speaker"))
	e.Decide(next, "На 30 секунд")
	want := []string{"user: Заведи таймер", "assistant: [yandex] На какое время поставить таймер?", "user: На 30 секунд"}
	if len(got) != 4 || got[0].Role != "system" || !slices.Equal(messageTexts(got[1:]), want) {
		t.Fatalf("Claude classifier saw %+v", got)
	}
}

func TestLocalRepliesAreTaggedByTheRouteThatGaveThem(t *testing.T) {
	seen := make(chan []llm.Message, 8)
	var route atomic.Value
	route.Store("home")
	e := decisionsEngine(t, routeContextConfig(), func(_ context.Context, _ string, m []llm.Message, _ []string) (llm.RouteAnswer, error) {
		seen <- slices.Clone(m)
		return llm.RouteAnswer{Route: route.Load().(string)}, nil
	}, func(context.Context, []llm.Message, bool) (string, error) { return "Ответ Клода", nil })
	e.Home = homeFunc(func(context.Context, string) (string, error) { return "Свет выключен.", nil })
	r := newSpeakerRig(t, e)
	r.say("Выключи свет")
	r.waitTurn("[home] Свет выключен.")
	route.Store("llm")
	r.say("Почему небо голубое")
	r.waitTurn("[llm] Ответ Клода")
	r.say("Сочини стих")
	<-seen
	<-seen
	want := []string{"user: Выключи свет", "assistant: [home] Свет выключен.", "user: Почему небо голубое", "assistant: [llm] Ответ Клода", "user: Сочини стих"}
	if got := messageTexts(<-seen); !slices.Equal(got, want) {
		t.Fatalf("Decisions saw %q, want %q", got, want)
	}
}

func TestStreamedYandexReplyIsRecordedWhole(t *testing.T) {
	e := decisionsEngine(t, routeContextConfig(), func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{Route: "yandex"}, nil
	}, failingClaude(t))
	r := newSpeakerRig(t, e)
	id := r.say("Заведи таймер")
	r.waitUp("TextInput")
	part := func(text string, last bool) {
		t.Helper()
		m := &server.TServerMessage{Message: &server.TServerMessage_Event{Event: &server.TServerMessage_TEvent{Header: &header.TEventHeader{RefMessageId: id}, Event: &server.TServerMessage_TEvent_DeferredAliceResponse{DeferredAliceResponse: &deferred.TDeferedAliceResponseProto{IsLast: last, TDeferredResponse: &deferred.TDeferedAliceResponseProto_BaseResponse{BaseResponse: &deferred.TDeferedAliceResponseProto_TBaseResponse{Text: new(text)}}}}}}}
		if err := r.n.Server(t.Context(), websocket.MessageBinary, aa(m)); err != nil {
			t.Fatal(err)
		}
	}
	part("На какое ", false)
	part("время?", true)
	r.waitTurn("[yandex] На какое время?")
}

func TestReplyGivenByClaudeAfterAliceRefusedIsTaggedLLM(t *testing.T) {
	e := decisionsEngine(t, routeContextConfig(), func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{Route: "yandex"}, nil
	}, func(context.Context, []llm.Message, bool) (string, error) { return "Ответ Клода", nil })
	r := newSpeakerRig(t, e)
	id := r.say("Сложный вопрос")
	r.waitUp("TextInput")
	r.yandexAnswers(id, "Я не могу помочь с этим.")
	r.waitTurn("[llm] Ответ Клода")
}

func TestPrivateAnswerNeverReachesOpenAIEvenAfterYandexClearedTheDialogue(t *testing.T) {
	var got []llm.Message
	e := decisionsEngine(t, routeContextConfig(), func(_ context.Context, _ string, m []llm.Message, _ []string) (llm.RouteAnswer, error) {
		got = slices.Clone(m)
		return llm.RouteAnswer{Route: "yandex"}, nil
	}, failingClaude(t))
	speaker := dialogue.WithSpeaker(t.Context(), "device:speaker")
	ctx := e.Prepare(speaker)
	_, _, ticket := e.dialogue().Start(ctx)
	e.dialogue().Commit(ctx, ticket, "найди в истории про пароль", "Нашла запись.", true)
	e.RecordRouteTurn(ctx, "llm", "найди в истории про пароль", "Нашла запись.", true)
	d := e.Decide(e.Prepare(speaker), "какая погода")
	if _, err := e.ResolveDecision(e.dialogue().Use(speaker, d.ticket), "какая погода", d); err != nil {
		t.Fatal(err)
	}
	if len(e.dialogue().View(speaker).Messages) != 0 {
		t.Fatal("Yandex route did not clear the answer dialogue")
	}
	e.Decide(e.Prepare(speaker), "сочини стих")
	if want := []string{"user: сочини стих"}; !slices.Equal(messageTexts(got), want) {
		t.Fatalf("Decisions saw %q", messageTexts(got))
	}
}

func TestWhileTheDialogueIsPrivateOpenAIGetsOnlyThePhrase(t *testing.T) {
	var got []llm.Message
	e := decisionsEngine(t, routeContextConfig(), func(_ context.Context, _ string, m []llm.Message, _ []string) (llm.RouteAnswer, error) {
		got = slices.Clone(m)
		return llm.RouteAnswer{Route: "yandex"}, nil
	}, failingClaude(t))
	speaker := dialogue.WithSpeaker(t.Context(), "device:speaker")
	e.RecordRouteTurn(e.Prepare(speaker), "yandex", "открытый вопрос", "открытый ответ", true)
	// The private answer was made but its delivery was never recorded.
	ctx := e.Prepare(speaker)
	_, _, ticket := e.dialogue().Start(ctx)
	e.dialogue().Commit(ctx, ticket, "найди в истории", "Нашла.", true)
	e.Decide(e.Prepare(speaker), "сочини стих")
	if want := []string{"user: сочини стих"}; !slices.Equal(messageTexts(got), want) {
		t.Fatalf("Decisions saw %q", messageTexts(got))
	}
}

func TestAnswerModelKeepsSeeingOnlyItsOwnDialogue(t *testing.T) {
	var answerInput []llm.Message
	e := &Engine{Answers: completionFunc(func(_ context.Context, m []llm.Message, _ bool) (string, error) {
		answerInput = slices.Clone(m)
		return "Ответ", nil
	}), LLM: completionFunc(func(context.Context, []llm.Message, bool) (string, error) {
		return `{"route":"llm"}`, nil
	})}
	if err := e.SetRouting(routeContextConfig()); err != nil {
		t.Fatal(err)
	}
	speaker := dialogue.WithSpeaker(t.Context(), "device:speaker")
	ctx := e.Prepare(speaker)
	e.RecordRouteTurn(ctx, "yandex", "Заведи таймер", "На какое время?", true)
	if _, err := e.Resolve(e.Prepare(speaker), "сочини стих"); err != nil {
		t.Fatal(err)
	}
	for _, m := range answerInput {
		if strings.Contains(m.Content, "таймер") || strings.Contains(m.Content, "[yandex]") {
			t.Fatalf("classifier record reached the answer model: %+v", answerInput)
		}
	}
}

func TestStartingANewConversationForgetsTheRecordToo(t *testing.T) {
	var got []llm.Message
	e := decisionsEngine(t, routeContextConfig(), func(_ context.Context, _ string, m []llm.Message, _ []string) (llm.RouteAnswer, error) {
		got = slices.Clone(m)
		return llm.RouteAnswer{Route: "yandex"}, nil
	}, failingClaude(t))
	speaker := dialogue.WithSpeaker(t.Context(), "device:speaker")
	e.RecordRouteTurn(e.Prepare(speaker), "yandex", "Заведи таймер", "На какое время?", true)
	reset := e.Prepare(speaker)
	if _, err := e.Resolve(reset, "начни новый разговор"); err != nil {
		t.Fatal(err)
	}
	e.RecordRouteTurn(reset, "llm", "начни новый разговор", "Начинаем новый разговор.", true)
	e.Decide(e.Prepare(speaker), "сочини стих")
	if want := []string{"user: сочини стих"}; !slices.Equal(messageTexts(got), want) {
		t.Fatalf("Decisions saw %q", messageTexts(got))
	}
}

func TestRouteContextOffKeepsTheOldClassifierInput(t *testing.T) {
	var got []llm.Message
	cfg := routeContextConfig()
	cfg.Classifier.RouteContext = false
	e := decisionsEngine(t, cfg, func(_ context.Context, _ string, m []llm.Message, _ []string) (llm.RouteAnswer, error) {
		got = slices.Clone(m)
		return llm.RouteAnswer{Route: "yandex"}, nil
	}, failingClaude(t))
	speaker := dialogue.WithSpeaker(t.Context(), "device:speaker")
	e.RecordRouteTurn(e.Prepare(speaker), "yandex", "Заведи таймер", "На какое время?", true)
	e.Decide(e.Prepare(speaker), "На 30 секунд")
	if want := []string{"user: На 30 секунд"}; !slices.Equal(messageTexts(got), want) {
		t.Fatalf("Decisions saw %q", messageTexts(got))
	}
}
