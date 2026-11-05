package bridge

// User story: Claude gives a long answer, so the bridge starts a streaming
// reply while it waits. The Station decides from that first envelope whether
// to listen after the speech, so a long llm answer must ask for it there, or
// the owner has to say «Алиса» again. Other routes keep the microphone closed:
// a home command or music has nothing to continue. Sound that is already
// queued keeps it closed too, so the microphone does not open over the music.

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
	"quasarbridge/internal/sound"
)

type routedSlowResolver struct {
	route string
	work  func(context.Context)
}

func (r routedSlowResolver) Decide(context.Context, string) RouteDecision {
	return RouteDecision{Route: r.route}
}
func (r routedSlowResolver) Resolve(ctx context.Context, s string) (Resolution, error) {
	return r.ResolveDecision(ctx, s, r.Decide(ctx, s))
}
func (r routedSlowResolver) ResolveDecision(ctx context.Context, _ string, _ RouteDecision) (Resolution, error) {
	if r.work != nil {
		r.work(ctx)
	}
	<-ctx.Done()
	return Resolution{Route: r.route}, ctx.Err()
}

func initialStreamListens(t *testing.T, r routedSlowResolver) bool {
	t.Helper()
	n, _, down := soundSession(t, r)
	n.ProgressAfter = 50 * time.Millisecond
	if err := n.Client(t.Context(), websocket.MessageBinary, soundInput("slow-"+r.route)); err != nil {
		t.Fatal(err)
	}
	for {
		f := nextSoundFrame(t, down)
		if answer := f.Server.GetEvent().GetAliceResponse(); answer != nil && answer.GetResponse().GetIsStreaming() {
			return answer.GetVoiceResponse().GetShouldListen()
		}
	}
}

func TestLongLLMAnswerKeepsTheMicrophoneOpenFromTheFirstEnvelope(t *testing.T) {
	if !initialStreamListens(t, routedSlowResolver{route: "llm"}) {
		t.Fatal("long llm answer did not ask to listen")
	}
}

func TestOtherRoutesDoNotOpenTheMicrophoneOnAStreamingReply(t *testing.T) {
	for _, route := range []string{"home", "music", "playback"} {
		t.Run(route, func(t *testing.T) {
			if initialStreamListens(t, routedSlowResolver{route: route}) {
				t.Fatal("non-llm route asked to listen")
			}
		})
	}
}

func TestQueuedSilentPlaybackKeepsTheMicrophoneClosedOnALongLLMAnswer(t *testing.T) {
	queue := func(ctx context.Context) {
		if err := sound.Play(ctx, sound.Request{URL: "https://audio.example/test.mp3"}); err != nil {
			t.Error(err)
		}
	}
	if initialStreamListens(t, routedSlowResolver{route: "llm", work: queue}) {
		t.Fatal("microphone opened over queued playback")
	}
}
