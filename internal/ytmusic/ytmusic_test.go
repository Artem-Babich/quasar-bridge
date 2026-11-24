package ytmusic

// User story: YouTube Music is the only music on the speaker. ytm-alice runs
// on the speaker and plays; the bridge understands the phrase and sends one
// panel command. The speaker then names the track, or says one of the
// approved sentences when YouTube cannot play it.

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type panelCall struct {
	Action string `json:"action"`
	Query  string `json:"query,omitempty"`
}

func panel(t *testing.T, reply func(panelCall) (int, any)) (*Client, *[]panelCall, *[]http.Header) {
	t.Helper()
	var calls []panelCall
	var headers []http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = append(headers, r.Header.Clone())
		var c panelCall
		if r.Method == "GET" && r.URL.Path == "/api/state" {
			c.Action = "state"
		} else if err := json.UnmarshalRead(r.Body, &c); err != nil || r.URL.Path != "/api/command" || r.Method != "POST" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		calls = append(calls, c)
		status, body := reply(c)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.MarshalWrite(w, body)
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL}, &calls, &headers
}

func playing(status string, artist, title string) map[string]any {
	return map[string]any{"state": map[string]any{"status": status, "index": 0, "queue": []any{map[string]any{"artists": artist, "title": title}}}}
}

func TestPanelCommandCarriesThePanelHeaders(t *testing.T) {
	c, _, headers := panel(t, func(panelCall) (int, any) { return 200, playing("buffering", "Кино", "Кукушка") })
	if _, err := c.Command(t.Context(), "play", "Кино"); err != nil {
		t.Fatal(err)
	}
	h := (*headers)[0]
	if h.Get("X-Ytm-Client") != "web" || h.Get("Origin") != c.BaseURL {
		t.Fatalf("panel would refuse the command: %v", h)
	}
}

func TestQueryKeepsOnlyWhatToPlay(t *testing.T) {
	for phrase, want := range map[string]Request{
		"Поставь песню Кино Кукушка":                {Action: "play", Query: "Кино Кукушка"},
		"Алиса, включи группу Сплин":                {Action: "play", Query: "Сплин"},
		"Включи Daft Punk на ютубе":                 {Action: "play", Query: "Daft Punk"},
		"включи мне музыку Земфиры пожалуйста":      {Action: "play", Query: "Земфиры"},
		"Включи музыку":                             {Action: "recommendations"},
		"поставь что-нибудь":                        {Action: "recommendations"},
		"Включи мои лайки":                          {Action: "play", Query: "мои лайки"},
		"включи лайки":                              {Action: "play", Query: "мои лайки"},
		"Поставь понравившиеся":                     {Action: "play", Query: "мои лайки"},
		"Включи альбом Кино Звезда по имени Солнце": {Action: "play", Query: "альбом Кино Звезда по имени Солнце"},
	} {
		if got := Parse(phrase); got != want {
			t.Errorf("%q -> %+v, want %+v", phrase, got, want)
		}
	}
}

func TestStartNamesTheTrackAndPlaysOnYouTube(t *testing.T) {
	c, calls, _ := panel(t, func(panelCall) (int, any) { return 200, playing("buffering", "Кино", "Кукушка") })
	answer, err := (&Player{Client: c}).StartMusic(t.Context(), "Поставь Кино Кукушка")
	if err != nil || answer != "Включаю: Кино — Кукушка." {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
	if got := *calls; len(got) != 1 || got[0] != (panelCall{Action: "play", Query: "Кино Кукушка"}) {
		t.Fatalf("calls=%+v", got)
	}
}

func TestPlainMusicStartsRecommendations(t *testing.T) {
	c, calls, _ := panel(t, func(panelCall) (int, any) { return 200, playing("buffering", "Сплин", "Выхода нет") })
	if _, err := (&Player{Client: c}).StartMusic(t.Context(), "Включи музыку"); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].Action != "recommendations" {
		t.Fatalf("calls=%+v", *calls)
	}
}

func TestStartFailuresSayApprovedSentences(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   any
		want   string
	}{
		{"not found", 422, map[string]any{"error": "треки не найдены"}, "Не удалось найти такую музыку на YouTube."},
		{"rate limit", 422, map[string]any{"error": "YouTube временно ограничил запросы с этого IP (429); повторите через 5 мин"}, "YouTube временно ограничил запросы, попробуй через несколько минут."},
		{"no account", 422, map[string]any{"error": "Подключи YouTube Music в настройках, чтобы открыть личную библиотеку."}, "Для лайков нужно войти в YouTube."},
		{"expired", 422, map[string]any{"error": "Вход в YouTube истёк. Открой настройки и загрузи свежий cookies.txt."}, "Для лайков нужно войти в YouTube."},
		{"other", 422, map[string]any{"error": "что-то ещё"}, "Не удалось включить музыку на YouTube."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _ := panel(t, func(panelCall) (int, any) { return tc.status, tc.body })
			answer, err := (&Player{Client: c}).StartMusic(t.Context(), "Включи мои лайки")
			if err != nil || answer != tc.want {
				t.Fatalf("answer=%q err=%v", answer, err)
			}
		})
	}
}

func TestStartSaysThePlayerIsDownWhenThePanelIsUnreachable(t *testing.T) {
	c := &Client{BaseURL: "http://127.0.0.1:1"}
	answer, err := (&Player{Client: c}).StartMusic(t.Context(), "Включи Кино")
	if err != nil || answer != "Плеер YouTube на колонке не отвечает." {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
}

func TestActiveFollowsThePlayerState(t *testing.T) {
	// A paused queue is quiet: pause/stop words go to Yandex and the mic works.
	for status, want := range map[string]bool{"playing": true, "paused": false, "buffering": true, "stopped": false, "error": false} {
		c, _, _ := panel(t, func(panelCall) (int, any) { return 200, playing(status, "a", "b") })
		if got := (&Player{Client: c}).Active(t.Context()); got != want {
			t.Errorf("status %s: active=%v", status, got)
		}
	}
}

func TestControlSendsTheCommandAndStaysSilent(t *testing.T) {
	c, calls, _ := panel(t, func(panelCall) (int, any) { return 200, playing("playing", "a", "b") })
	silenced := 0
	p := &Player{Client: c, Silence: func(context.Context) error { silenced++; return nil }}
	for _, action := range []string{"pause", "resume", "next", "previous", "stop"} {
		if answer, err := p.Control(t.Context(), action); err != nil || silenced == 0 {
			t.Fatalf("%s: answer=%q err=%v silenced=%d", action, answer, err, silenced)
		}
	}
	var got []string
	for _, call := range *calls {
		if call.Action != "state" {
			got = append(got, call.Action)
		}
	}
	if strings.Join(got, ",") != "pause,resume,next,previous,stop" {
		t.Fatalf("calls=%v", got)
	}
}

func TestControlWithoutSpeakerContextSaysOK(t *testing.T) {
	c, _, _ := panel(t, func(panelCall) (int, any) { return 200, playing("playing", "a", "b") })
	p := &Player{Client: c, Silence: func(context.Context) error { return context.Canceled }}
	if answer, _ := p.Control(t.Context(), "pause"); answer != "Хорошо." {
		t.Fatalf("answer=%q", answer)
	}
}

func TestWhatIsPlayingNamesTheTrackOrSilence(t *testing.T) {
	c, _, _ := panel(t, func(panelCall) (int, any) { return 200, playing("playing", "Кино", "Кукушка") })
	if answer, _ := (&Player{Client: c}).Control(t.Context(), "what"); answer != "Сейчас играет: Кино — Кукушка." {
		t.Fatalf("answer=%q", answer)
	}
	c, _, _ = panel(t, func(panelCall) (int, any) { return 200, playing("stopped", "Кино", "Кукушка") })
	if answer, _ := (&Player{Client: c}).Control(t.Context(), "what"); answer != "Сейчас ничего не играет." {
		t.Fatalf("answer=%q", answer)
	}
}

func TestIdleControlsAnswerWithoutTouchingYandex(t *testing.T) {
	// The real panel starts an empty queue silently, so the bridge checks first.
	c, calls, _ := panel(t, func(c panelCall) (int, any) {
		return 200, map[string]any{"state": map[string]any{"status": "stopped", "queue": []any{}}}
	})
	p := &Player{Client: c, Silence: func(context.Context) error { return nil }}
	for action, want := range map[string]string{"next": "Сейчас ничего не играет.", "previous": "Сейчас ничего не играет.", "resume": "Нечего продолжать."} {
		if !p.HandlesIdle(action) {
			t.Fatalf("%s must be answered by YouTube when idle", action)
		}
		if answer, _ := p.Control(t.Context(), action); answer != want {
			t.Fatalf("%s: answer=%q", action, answer)
		}
	}
	for _, action := range []string{"pause", "stop"} {
		if p.HandlesIdle(action) {
			t.Fatalf("%s while idle belongs to Yandex: an alarm or timer may ring", action)
		}
	}
	for _, call := range *calls {
		if call.Action != "state" {
			t.Fatalf("idle %s reached the player", call.Action)
		}
	}
}

func TestResumeWhileIdleContinuesTheSavedQueue(t *testing.T) {
	c, calls, _ := panel(t, func(panelCall) (int, any) { return 200, playing("stopped", "Кино", "Кукушка") })
	p := &Player{Client: c, Silence: func(context.Context) error { return nil }}
	if answer, _ := p.Control(t.Context(), "resume"); answer != "" {
		t.Fatalf("answer=%q", answer)
	}
	if last := (*calls)[len(*calls)-1]; last.Action != "resume" {
		t.Fatalf("saved queue was not resumed: %+v", *calls)
	}
}

func TestHungPanelDoesNotHoldVoiceRequests(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	t.Cleanup(func() { close(block); srv.Close() })
	start := time.Now()
	if (&Player{Client: &Client{BaseURL: srv.URL}}).Active(t.Context()) {
		t.Fatal("a hung panel counted as playing")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("a hung panel held the request for %v", elapsed)
	}
}

func TestMoreFailureSentences(t *testing.T) {
	for msg, want := range map[string]string{"альбом или плейлист не найден": "Не удалось найти такую музыку на YouTube.", "достигнут край очереди": "Это последний трек в очереди."} {
		if got := failureText(&PanelError{Status: 422, Message: msg}); got != want {
			t.Errorf("%q -> %q", msg, got)
		}
	}
}

func TestLikesDuringMusicAreIgnoredSilently(t *testing.T) {
	c, calls, _ := panel(t, func(panelCall) (int, any) { return 200, playing("playing", "a", "b") })
	silenced := 0
	p := &Player{Client: c, Silence: func(context.Context) error { silenced++; return nil }}
	if _, err := p.Control(t.Context(), "ignore"); err != nil || silenced != 1 {
		t.Fatalf("err=%v silenced=%d", err, silenced)
	}
	for _, call := range *calls {
		if call.Action != "state" {
			t.Fatalf("ignored word sent %q", call.Action)
		}
	}
}
