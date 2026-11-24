package bridge

// User story: YouTube Music is the only music on the speaker (owner
// requirement 2026-11-04, plan A 2026-11-07). The route classifier may pick
// «music» only when the YouTube player is configured; a music phrase then
// plays on YouTube and never reaches Yandex. Player words control YouTube,
// the Claude conversation survives a music command, and the microphone does
// not reopen over the music.

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/skrashevich/quasar-bridge/internal/dialogue"
	"github.com/skrashevich/quasar-bridge/internal/llm"
	"github.com/skrashevich/quasar-bridge/internal/reply"
)

type youTubeFixture struct {
	asked   int
	active  bool
	started []string
	actions []string
}

func (y *youTubeFixture) CanStartMusic(string) bool { return false }
func (y *youTubeFixture) StartMusic(_ context.Context, text string) (string, error) {
	y.started = append(y.started, text)
	y.active = true
	return "Включаю: Кино — Кукушка.", nil
}
func (y *youTubeFixture) Active(context.Context) bool { y.asked++; return y.active }
func (y *youTubeFixture) Control(_ context.Context, action string) (string, error) {
	y.actions = append(y.actions, action)
	return "", nil
}
func (y *youTubeFixture) HandlesIdle(action string) bool {
	return action == "next" || action == "previous" || action == "resume" || action == "what"
}

func youTubeEngine(t *testing.T, chooser chooserFunc, claude completionFunc) (*Engine, *youTubeFixture) {
	t.Helper()
	yt := &youTubeFixture{}
	e := decisionsEngine(t, DefaultRoutingConfig(), chooser, claude)
	e.Music, e.Playback, e.MusicRoute = yt, yt, true
	return e, yt
}

func TestClassifierMusicChoicePlaysOnYouTube(t *testing.T) {
	var routes []string
	e, yt := youTubeEngine(t, func(_ context.Context, _ string, _ []llm.Message, r []string) (llm.RouteAnswer, error) {
		routes = r
		return llm.RouteAnswer{Route: "music"}, nil
	}, failingClaude(t))
	ctx := e.Prepare(dialogue.WithSpeaker(t.Context(), "device:speaker"))
	result, err := e.Resolve(ctx, "Поставь Кино Кукушка")
	if err != nil || result.Route != "music" || result.Answer != "Включаю: Кино — Кукушка." {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !slices.Contains(routes, "music") || len(yt.started) != 1 {
		t.Fatalf("routes=%v started=%v", routes, yt.started)
	}
}

func TestWithoutYouTubeTheClassifierCannotChooseMusic(t *testing.T) {
	var routes []string
	e := decisionsEngine(t, DefaultRoutingConfig(), func(_ context.Context, _ string, _ []llm.Message, r []string) (llm.RouteAnswer, error) {
		routes = r
		return llm.RouteAnswer{Route: "llm"}, nil
	}, failingClaude(t))
	e.Decide(t.Context(), "Поставь Кино")
	if slices.Contains(routes, "music") {
		t.Fatalf("routes=%v", routes)
	}
}

func TestClaudeClassifierFallbackMayChooseMusic(t *testing.T) {
	e, _ := youTubeEngine(t, func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{}, &llm.DecisionsError{Class: "timeout"}
	}, func(context.Context, []llm.Message, bool) (string, error) { return `{"route":"music"}`, nil })
	if d := e.Decide(t.Context(), "Включи Сплин"); d.Route != "music" {
		t.Fatalf("decision=%+v", d)
	}
}

func TestLaunchWordWithBothClassifiersDownDoesNotGoToYandex(t *testing.T) {
	cfg := DefaultRoutingConfig()
	cfg.Classifier.OnError = "yandex"
	e := decisionsEngine(t, cfg, func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{}, &llm.DecisionsError{Class: "timeout"}
	}, func(context.Context, []llm.Message, bool) (string, error) { return "", errors.New("down") })
	yt := &youTubeFixture{}
	e.Music, e.Playback, e.MusicRoute = yt, yt, true
	result, err := e.Resolve(t.Context(), "Включи Кино")
	if err != nil || result.Route == "yandex" || result.Answer != "Не удалось понять, что включить. Повтори, пожалуйста." {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if other, _ := e.Resolve(t.Context(), "Сколько градусов на улице"); other.Route != "yandex" {
		t.Fatalf("a phrase without a launch word must keep on_error: %+v", other)
	}
}

func TestMusicCommandKeepsTheClaudeConversation(t *testing.T) {
	route := "llm"
	e, _ := youTubeEngine(t, func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{Route: route}, nil
	}, func(context.Context, []llm.Message, bool) (string, error) { return "Рецепт борща.", nil })
	speaker := dialogue.WithSpeaker(t.Context(), "device:speaker")
	if _, err := e.Resolve(e.Prepare(speaker), "Как сварить борщ"); err != nil {
		t.Fatal(err)
	}
	route = "music"
	if _, err := e.Resolve(e.Prepare(speaker), "Поставь Кино"); err != nil {
		t.Fatal(err)
	}
	_, snapshot, _ := e.dialogue().Start(e.Prepare(speaker))
	if len(snapshot.Messages) == 0 {
		t.Fatal("music command erased the conversation with Claude")
	}
}

func TestMicrophoneStaysClosedWhileYouTubePlays(t *testing.T) {
	e, yt := youTubeEngine(t, func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{Route: "llm"}, nil
	}, func(context.Context, []llm.Message, bool) (string, error) { return "Ответ.", nil })
	yt.active = true
	ctx := reply.WithPolicy(e.Prepare(dialogue.WithSpeaker(t.Context(), "device:speaker")))
	if _, err := e.Resolve(ctx, "Почему небо голубое"); err != nil {
		t.Fatal(err)
	}
	if reply.ListeningAllowed(ctx) {
		t.Fatal("microphone would reopen over the music")
	}
	yt.active = false
	quiet := reply.WithPolicy(e.Prepare(dialogue.WithSpeaker(t.Context(), "device:speaker")))
	if _, err := e.Resolve(quiet, "Почему трава зелёная"); err != nil {
		t.Fatal(err)
	}
	if !reply.ListeningAllowed(quiet) {
		t.Fatal("without music the microphone must behave as before")
	}
}

func TestPlayerWordsWhileIdleAreAnsweredByYouTube(t *testing.T) {
	e, yt := youTubeEngine(t, func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{Route: "yandex"}, nil
	}, failingClaude(t))
	for _, phrase := range []string{"Дальше", "Назад", "Продолжи", "Что играет"} {
		if d := e.Decide(t.Context(), phrase); d.Route != "playback" {
			t.Fatalf("%q idle: %+v", phrase, d)
		}
	}
	for _, phrase := range []string{"Пауза", "Хватит"} {
		if d := e.Decide(t.Context(), phrase); d.Route == "playback" {
			t.Fatalf("%q idle must stay with Yandex (alarm, timer): %+v", phrase, d)
		}
	}
	yt.active = true
	if d := e.Decide(t.Context(), "Хватит"); d.Route != "playback" {
		t.Fatalf("«хватит» during music must stop YouTube: %+v", d)
	}
}

func TestYandexRequestsDoNotWaitForThePlayer(t *testing.T) {
	e, yt := youTubeEngine(t, func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{Route: "yandex"}, nil
	}, failingClaude(t))
	if _, err := e.Resolve(t.Context(), "Заведи таймер на минуту"); err != nil {
		t.Fatal(err)
	}
	if yt.asked != 0 {
		t.Fatalf("a Yandex request asked the player %d times", yt.asked)
	}
}

func TestYouTubeRepliesAreTaggedMusicForTheClassifier(t *testing.T) {
	cfg := DefaultRoutingConfig()
	cfg.Classifier.RouteContext = true
	e := decisionsEngine(t, cfg, func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{Route: "llm"}, nil
	}, failingClaude(t))
	e.MusicRoute = true
	ctx := e.Prepare(dialogue.WithSpeaker(t.Context(), "device:speaker"))
	e.RecordRouteTurn(ctx, "music", "Поставь Кино", "Включаю: Кино — Кукушка.", true)
	got := messageTexts(e.dialogue().RouteContext(e.Prepare(dialogue.WithSpeaker(t.Context(), "device:speaker"))).Messages)
	if !slices.Contains(got, "assistant: [music] Включаю: Кино — Кукушка.") {
		t.Fatalf("route context %q", got)
	}
}

func TestIdlePlayerWordsBelongToAnOpenClaudeConversation(t *testing.T) {
	route := "llm"
	e, _ := youTubeEngine(t, func(context.Context, string, []llm.Message, []string) (llm.RouteAnswer, error) {
		return llm.RouteAnswer{Route: route}, nil
	}, func(context.Context, []llm.Message, bool) (string, error) {
		return "Первая часть рассказа.", nil
	})
	speaker := dialogue.WithSpeaker(t.Context(), "device:speaker")
	if _, err := e.Resolve(e.Prepare(speaker), "Расскажи длинную историю"); err != nil {
		t.Fatal(err)
	}
	if d := e.Decide(e.Prepare(speaker), "Продолжай"); d.Route == "playback" {
		t.Fatalf("«продолжай» in a conversation went to the idle player: %+v", d)
	}
}
