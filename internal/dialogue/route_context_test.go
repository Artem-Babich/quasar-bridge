package dialogue

// User story: the route classifiers must see what was just said on this
// speaker, whichever handler answered. Yandex asks «На какое время поставить
// таймер?», the owner says «На 30 секунд», and the classifier has to know who
// asked. It gets at most the last 3 turns from the last 2 minutes, each reply
// tagged with its handler. Claude's own answer dialogue is a separate memory:
// a Yandex or home turn neither adds to it nor is it restored by this record.

import (
	"context"
	"strings"
	"testing"
	"time"

	"quasarbridge/internal/llm"
)

func routeStore(t *testing.T) (*Store, context.Context, *time.Time) {
	t.Helper()
	s := New(DefaultConfig())
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }
	return s, WithSpeaker(t.Context(), "speaker"), &now
}

func recordTurn(ctx context.Context, s *Store, route, user, answer string) {
	_, ticket := s.Begin(ctx)
	s.CaptureRouteTurn(ctx, ticket, route, user, answer, true, false)
}

func wantMessages(t *testing.T, got []llm.Message, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("messages=%+v, want %q", got, want)
	}
	for i, m := range got {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if m.Role != role || m.Content != want[i] {
			t.Fatalf("message %d=%+v, want %s %q", i, m, role, want[i])
		}
	}
}

func TestClassifierContextTagsEachReplyWithItsRoute(t *testing.T) {
	s, ctx, _ := routeStore(t)
	recordTurn(ctx, s, "yandex", "Заведи таймер", "На какое время поставить таймер?")
	recordTurn(ctx, s, "home", "Выключи свет", "Свет выключен.")
	recordTurn(ctx, s, "llm", "Почему небо голубое", "Из-за рассеяния.")
	wantMessages(t, s.RouteContext(ctx).Messages,
		"Заведи таймер", "[yandex] На какое время поставить таймер?",
		"Выключи свет", "[home] Свет выключен.",
		"Почему небо голубое", "[llm] Из-за рассеяния.")
}

func TestClassifierContextKeepsOnlyTheLastThreeTurns(t *testing.T) {
	s, ctx, _ := routeStore(t)
	for _, q := range []string{"первый", "второй", "третий", "четвёртый"} {
		recordTurn(ctx, s, "yandex", q, "ответ "+q)
	}
	wantMessages(t, s.RouteContext(ctx).Messages,
		"второй", "[yandex] ответ второй",
		"третий", "[yandex] ответ третий",
		"четвёртый", "[yandex] ответ четвёртый")
}

func TestClassifierContextForgetsTurnsOlderThanTwoMinutes(t *testing.T) {
	s, ctx, now := routeStore(t)
	recordTurn(ctx, s, "yandex", "старый", "старый ответ")
	*now = now.Add(time.Minute)
	recordTurn(ctx, s, "home", "свежий", "свежий ответ")
	*now = now.Add(time.Minute - time.Second)
	wantMessages(t, s.RouteContext(ctx).Messages,
		"старый", "[yandex] старый ответ", "свежий", "[home] свежий ответ")
	*now = now.Add(time.Second)
	wantMessages(t, s.RouteContext(ctx).Messages, "свежий", "[home] свежий ответ")
}

func TestClassifierContextJoinsStreamedPartsIntoOneReply(t *testing.T) {
	s, ctx, _ := routeStore(t)
	_, ticket := s.Begin(ctx)
	s.CaptureRouteTurn(ctx, ticket, "yandex", "Расскажи", "", false, false)
	s.CaptureRouteTurn(ctx, ticket, "yandex", "Расскажи", "Часть ", false, false)
	if got := s.RouteContext(ctx).Messages; len(got) != 0 {
		t.Fatalf("unfinished reply reached classifier context: %+v", got)
	}
	s.CaptureRouteTurn(ctx, ticket, "yandex", "Расскажи", "ответа.", true, false)
	wantMessages(t, s.RouteContext(ctx).Messages, "Расскажи", "[yandex] Часть ответа.")
}

func TestClassifierContextRecordsOneReplyOnlyOnce(t *testing.T) {
	s, ctx, _ := routeStore(t)
	_, ticket := s.Begin(ctx)
	s.CaptureRouteTurn(ctx, ticket, "yandex", "Вопрос", "Ответ", true, false)
	s.CaptureRouteTurn(ctx, ticket, "yandex", "Вопрос", "Ответ", true, false)
	wantMessages(t, s.RouteContext(ctx).Messages, "Вопрос", "[yandex] Ответ")
}

func TestSilentReplyLeavesNoTurnForTheClassifier(t *testing.T) {
	s, ctx, _ := routeStore(t)
	recordTurn(ctx, s, "yandex", "Стоп", "  ")
	if got := s.RouteContext(ctx).Messages; len(got) != 0 {
		t.Fatalf("empty reply stored: %+v", got)
	}
}

func TestReplyFromAnOlderRequestIsNotRecorded(t *testing.T) {
	s, ctx, _ := routeStore(t)
	_, late := s.Begin(ctx)
	s.Begin(ctx)
	s.CaptureRouteTurn(ctx, late, "yandex", "старый", "поздний ответ", true, false)
	if got := s.RouteContext(ctx).Messages; len(got) != 0 {
		t.Fatalf("stale reply stored: %+v", got)
	}
}

func TestClassifierContextIsSeparatePerSpeaker(t *testing.T) {
	s, ctx, _ := routeStore(t)
	recordTurn(ctx, s, "yandex", "Заведи таймер", "На какое время?")
	other := WithSpeaker(t.Context(), "other")
	if got := s.RouteContext(other).Messages; len(got) != 0 {
		t.Fatalf("another speaker saw the turn: %+v", got)
	}
}

func TestPrivateTurnMarksClassifierContextPrivateUntilItLeaves(t *testing.T) {
	s, ctx, _ := routeStore(t)
	_, ticket := s.Begin(ctx)
	s.CaptureRouteTurn(ctx, ticket, "llm", "найди в истории", "Нашла запись.", true, true)
	if !s.RouteContext(ctx).PrivateHistory {
		t.Fatal("private turn not marked")
	}
	for _, q := range []string{"один", "два", "три"} {
		recordTurn(ctx, s, "yandex", q, "ответ")
	}
	if s.RouteContext(ctx).PrivateHistory {
		t.Fatal("turn outside the window kept the context private")
	}
}

func TestClearingAnswerDialogueKeepsClassifierContext(t *testing.T) {
	s, ctx, _ := routeStore(t)
	recordTurn(ctx, s, "yandex", "Заведи таймер", "На какое время?")
	_, ticket := s.Begin(ctx)
	if !s.Clear(s.Use(ctx, ticket), ticket) {
		t.Fatal("clear refused")
	}
	wantMessages(t, s.RouteContext(ctx).Messages, "Заведи таймер", "[yandex] На какое время?")
}

func TestRouteTurnNeverEntersClaudesAnswerDialogue(t *testing.T) {
	s, ctx, _ := routeStore(t)
	recordTurn(ctx, s, "yandex", "Заведи таймер", "На какое время?")
	if got := s.View(ctx).Messages; len(got) != 0 {
		t.Fatalf("answer dialogue changed: %+v", got)
	}
}

func TestClearRouteContextForgetsEverything(t *testing.T) {
	s, ctx, _ := routeStore(t)
	recordTurn(ctx, s, "yandex", "Заведи таймер", "На какое время?")
	_, ticket := s.Begin(ctx)
	if !s.ClearRouteContext(ctx, ticket) {
		t.Fatal("clear refused")
	}
	if got := s.RouteContext(ctx).Messages; len(got) != 0 {
		t.Fatalf("context survived: %+v", got)
	}
}

func TestOversizedReplyIsNotKeptForTheClassifier(t *testing.T) {
	s, ctx, _ := routeStore(t)
	recordTurn(ctx, s, "yandex", "короткий", "ответ")
	recordTurn(ctx, s, "llm", "длинный", strings.Repeat("я", DefaultConfig().MaxChars+1))
	wantMessages(t, s.RouteContext(ctx).Messages, "короткий", "[yandex] ответ")
}

func TestExpiredDialogueDropsClassifierContext(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TTLSeconds = 30
	s := New(cfg)
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }
	ctx := WithSpeaker(t.Context(), "speaker")
	recordTurn(ctx, s, "yandex", "вопрос", "ответ")
	now = now.Add(31 * time.Second)
	if got := s.RouteContext(ctx); len(got.Messages) != 0 {
		t.Fatalf("expired entry kept context: %+v", got)
	}
	s.Begin(ctx)
	if got := s.RouteContext(ctx); len(got.Messages) != 0 {
		t.Fatalf("next request revived expired context: %+v", got)
	}
}
