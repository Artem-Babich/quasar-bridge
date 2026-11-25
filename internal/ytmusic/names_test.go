package ytmusic

// User story (owner, 2026-11-25): «включи Операцию Пластилин» found nothing,
// because ytm-alice matches words exactly and the phrase says «операцию».
// «включи группу Порнофильмы» queued a song titled «Порнофильмы» by another
// artist. Before searching, a language model names the artist and the song
// the way the catalog spells them (nominative case, original spelling). A
// request naming only the artist plays only that artist's songs. The words
// as spoken remain a fallback search. When the model is slow, fails or
// answers nonsense, the search works as before. «Мои лайки» and «включи
// музыку» never ask the model.

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/skrashevich/quasar-bridge/internal/llm"
)

type playCall struct {
	Action string `json:"action"`
	Query  string `json:"query,omitempty"`
	Artist string `json:"artist,omitempty"`
	Title  string `json:"title,omitempty"`
	Spoken string `json:"spoken,omitempty"`
}

func playPanel(t *testing.T) (*Client, *[]playCall) {
	t.Helper()
	var calls []playCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c playCall
		if err := json.UnmarshalRead(r.Body, &c); err != nil {
			t.Errorf("bad command: %v", err)
		}
		calls = append(calls, c)
		w.Header().Set("Content-Type", "application/json")
		json.MarshalWrite(w, playing("buffering", "Операция Пластилин", "Ночь"))
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL}, &calls
}

type modelFunc func(context.Context, []llm.Message, bool) (string, error)

func (f modelFunc) Complete(ctx context.Context, m []llm.Message, j bool) (string, error) {
	return f(ctx, m, j)
}

func answering(answer string, asked *[]string) modelFunc {
	return func(_ context.Context, m []llm.Message, _ bool) (string, error) {
		*asked = append(*asked, m[len(m)-1].Content)
		return answer, nil
	}
}

func TestArtistNamedInAnotherCaseIsSearchedInTheNominative(t *testing.T) {
	c, calls := playPanel(t)
	var asked []string
	p := &Player{Client: c, Model: answering(`{"artist":"Операция Пластилин","title":""}`, &asked)}
	if _, err := p.StartMusic(t.Context(), "Включи Операцию Пластилин"); err != nil {
		t.Fatal(err)
	}
	want := playCall{Action: "play", Query: "Операция Пластилин", Artist: "Операция Пластилин", Spoken: "Операцию Пластилин"}
	if len(*calls) != 1 || (*calls)[0] != want {
		t.Fatalf("calls %+v, want %+v", *calls, want)
	}
	if len(asked) != 1 || asked[0] != "Включи Операцию Пластилин" {
		t.Fatalf("model asked %q", asked)
	}
}

func TestArtistAndSongAreBothSent(t *testing.T) {
	c, calls := playPanel(t)
	var asked []string
	p := &Player{Client: c, Model: answering(`{"artist":"Кино","title":"Кукушка"}`, &asked)}
	if _, err := p.StartMusic(t.Context(), "Поставь песню Кукушка группы Кино"); err != nil {
		t.Fatal(err)
	}
	got := (*calls)[0]
	if got.Query != "Кино Кукушка" || got.Artist != "Кино" || got.Title != "Кукушка" {
		t.Fatalf("call %+v", got)
	}
}

func TestSpokenFallbackIsLeftOutWhenItEqualsTheQuery(t *testing.T) {
	c, calls := playPanel(t)
	var asked []string
	p := &Player{Client: c, Model: answering(`{"artist":"Сплин","title":""}`, &asked)}
	if _, err := p.StartMusic(t.Context(), "Включи группу Сплин"); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[0]; got.Spoken != "" || got.Query != "Сплин" || got.Artist != "Сплин" {
		t.Fatalf("call %+v", got)
	}
}

func TestWithoutUsableNamesTheSearchWorksAsBefore(t *testing.T) {
	slow := modelFunc(func(ctx context.Context, _ []llm.Message, _ bool) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	for name, model := range map[string]Completer{
		"error":    modelFunc(func(context.Context, []llm.Message, bool) (string, error) { return "", errors.New("down") }),
		"nonsense": modelFunc(func(context.Context, []llm.Message, bool) (string, error) { return "не знаю", nil }),
		"empty":    modelFunc(func(context.Context, []llm.Message, bool) (string, error) { return `{"artist":"","title":""}`, nil }),
		"slow":     slow,
		"none":     nil,
	} {
		c, calls := playPanel(t)
		p := &Player{Client: c, Model: model, NamesTimeout: 50 * time.Millisecond}
		start := time.Now()
		if _, err := p.StartMusic(t.Context(), "Включи Операцию Пластилин"); err != nil {
			t.Fatal(name, err)
		}
		if time.Since(start) > 2*time.Second {
			t.Fatalf("%s: a slow model held the music for %v", name, time.Since(start))
		}
		want := playCall{Action: "play", Query: "Операцию Пластилин"}
		if len(*calls) != 1 || (*calls)[0] != want {
			t.Fatalf("%s: calls %+v, want %+v", name, *calls, want)
		}
	}
}

func TestLikesAndPlainMusicNeverAskTheModel(t *testing.T) {
	for _, phrase := range []string{"Включи мои лайки", "Включи музыку"} {
		c, _ := playPanel(t)
		var asked []string
		p := &Player{Client: c, Model: answering(`{"artist":"X","title":""}`, &asked)}
		if _, err := p.StartMusic(t.Context(), phrase); err != nil {
			t.Fatal(err)
		}
		if len(asked) != 0 {
			t.Fatalf("%q asked the model", phrase)
		}
	}
}

func TestNamesPromptAsksForCatalogSpellingAndJSON(t *testing.T) {
	var system string
	var wantJSON bool
	model := modelFunc(func(_ context.Context, m []llm.Message, j bool) (string, error) {
		system, wantJSON = m[0].Content, j
		return `{"artist":"Кино","title":""}`, nil
	})
	c, _ := playPanel(t)
	p := &Player{Client: c, Model: model}
	if _, err := p.StartMusic(t.Context(), "Включи Кино"); err != nil {
		t.Fatal(err)
	}
	if !wantJSON || !strings.Contains(system, "именительн") || !strings.Contains(system, `"artist"`) {
		t.Fatalf("json=%v prompt=%q", wantJSON, system)
	}
}

// ytm-alice plays a whole album or playlist for a query starting with
// «альбом» or «плейлист»; the model must not rewrite such a query.
func TestAlbumAndPlaylistQueriesKeepTheirPrefix(t *testing.T) {
	for phrase, query := range map[string]string{
		"Включи альбом Кино Звезда по имени Солнце": "альбом Кино Звезда по имени Солнце",
		"Включи плейлист Для бега":                  "плейлист Для бега",
	} {
		c, calls := playPanel(t)
		var asked []string
		p := &Player{Client: c, Model: answering(`{"artist":"Кино","title":"Звезда по имени Солнце"}`, &asked)}
		if _, err := p.StartMusic(t.Context(), phrase); err != nil {
			t.Fatal(err)
		}
		if len(asked) != 0 || (*calls)[0].Query != query {
			t.Fatalf("%q: asked=%q call=%+v", phrase, asked, (*calls)[0])
		}
	}
}

// Haiku sometimes wraps the JSON in a ```json fence (seen on Pi 3).
func TestFencedModelAnswerIsAccepted(t *testing.T) {
	c, calls := playPanel(t)
	var asked []string
	p := &Player{Client: c, Model: answering("```json\n{\"artist\":\"Земфира\",\"title\":\"\"}\n```", &asked)}
	if _, err := p.StartMusic(t.Context(), "Включи Земфиру"); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[0]; got.Query != "Земфира" || got.Artist != "Земфира" {
		t.Fatalf("call %+v", got)
	}
}

// A name like «Операция Пластилин» took Haiku 2.4–3 s on Pi 3; the default
// wait must cover it.
func TestDefaultNamesWaitCoversASlowName(t *testing.T) {
	if namesTimeout < 3500*time.Millisecond {
		t.Fatalf("names timeout %v", namesTimeout)
	}
}
