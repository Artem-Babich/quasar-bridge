package ytmusic

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/skrashevich/quasar-bridge/internal/llm"
)

// Approved sentences (owner decision 2026-11-07).
const (
	sayNotFound   = "Не удалось найти такую музыку на YouTube."
	sayRateLimit  = "YouTube временно ограничил запросы, попробуй через несколько минут."
	sayNeedLogin  = "Для лайков нужно войти в YouTube."
	sayPlayerDown = "Плеер YouTube на колонке не отвечает."
	sayNothing    = "Сейчас ничего не играет."
	sayNoResume   = "Нечего продолжать."
	sayFailed     = "Не удалось включить музыку на YouTube."
	sayQueueEdge  = "Это последний трек в очереди."
	sayOK         = "Хорошо."
)

// Player puts ytm-alice in the bridge's music and playback slots.
type Player struct {
	Client *Client
	// Silence makes the current speaker reply silent (an audio_stop reply
	// without speech). Without it, or when it fails, the reply is «Хорошо.».
	Silence func(context.Context) error
	// Model names the artist and the song before the search (names.go).
	// Without it the spoken words are searched as they are.
	Model Completer
	// NamesTimeout bounds the model; zero means namesTimeout.
	NamesTimeout time.Duration
}

// Completer is the language model that names the artist and the song.
type Completer interface {
	Complete(context.Context, []llm.Message, bool) (string, error)
}

// CanStartMusic is false: the route classifier chooses the music route.
func (p *Player) CanStartMusic(string) bool { return false }

// StartMusic plays the phrase on YouTube Music and names the track. Known
// failures become approved sentences, not errors.
func (p *Player) StartMusic(ctx context.Context, text string) (string, error) {
	req := Parse(text)
	command := Command{Action: req.Action, Query: req.Query}
	if namesHelp(req) {
		if n, ok := p.names(ctx, text); ok {
			command.Artist, command.Title = n.Artist, n.Title
			command.Query = strings.TrimSpace(n.Artist + " " + n.Title)
			if req.Query != command.Query {
				command.Spoken = req.Query
			}
		}
	}
	slog.Info("ytmusic_start", "action", command.Action, "query", command.Query, "artist", command.Artist, "title", command.Title)
	state, err := p.Client.Send(ctx, command)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		slog.Warn("ytmusic_start_failed", "action", req.Action, "error", err)
		return failureText(err), nil
	}
	if track, ok := state.Current(); ok && track.Title != "" {
		if track.Artists != "" {
			return "Включаю: " + track.Artists + " — " + track.Title + ".", nil
		}
		return "Включаю: " + track.Title + ".", nil
	}
	return "Включаю.", nil
}

func failureText(err error) string {
	var refused *PanelError
	if !errors.As(err, &refused) {
		return sayPlayerDown
	}
	m := strings.ToLower(refused.Message)
	switch {
	case strings.Contains(m, "(429)") || strings.Contains(m, "ограничил"):
		return sayRateLimit
	case strings.Contains(m, "не найден"):
		return sayNotFound
	case strings.Contains(m, "край очереди"):
		return sayQueueEdge
	case strings.Contains(m, "подключи youtube") || strings.Contains(m, "вход в youtube"):
		return sayNeedLogin
	case refused.Status >= 500:
		return sayPlayerDown
	}
	return sayFailed
}

// Active reports music sounding now. A paused queue is quiet: «пауза»,
// «хватит» then go to Yandex (an alarm may ring) and the microphone works.
// The panel gets one second: every voice request asks.
func (p *Player) Active(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	state, err := p.Client.Current(ctx)
	return err == nil && state.Sounding()
}

// HandlesIdle names the controls YouTube answers while nothing plays. Pause
// and stop stay with Yandex then: an alarm or a timer may be ringing.
func (p *Player) HandlesIdle(action string) bool {
	return action == "next" || action == "previous" || action == "resume" || action == "what"
}

// Control applies a player word. Commands are silent; questions are spoken.
func (p *Player) Control(ctx context.Context, action string) (string, error) {
	state, err := p.Client.Current(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return sayPlayerDown, nil
	}
	switch action {
	case "what":
		if track, ok := state.Current(); ok && state.Playing() && track.Title != "" {
			if track.Artists != "" {
				return "Сейчас играет: " + track.Artists + " — " + track.Title + ".", nil
			}
			return "Сейчас играет: " + track.Title + ".", nil
		}
		return sayNothing, nil
	case "ignore":
		return p.silent(ctx), nil
	case "next", "previous":
		if !state.Playing() {
			return sayNothing, nil
		}
	case "restart":
		action = "seek"
	case "resume":
		if !state.Playing() && len(state.Queue) == 0 {
			return sayNoResume, nil
		}
	case "pause", "stop":
	default:
		return "", errors.New("unknown YouTube Music control")
	}
	// seek without a position returns to the start of the track.
	if _, err := p.Client.Command(ctx, action, ""); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		var refused *PanelError
		if action == "resume" && errors.As(err, &refused) {
			return sayNoResume, nil
		}
		slog.Warn("ytmusic_control_failed", "action", action, "error", err)
		return failureText(err), nil
	}
	return p.silent(ctx), nil
}

func (p *Player) silent(ctx context.Context) string {
	if p.Silence != nil && p.Silence(ctx) == nil {
		return ""
	}
	return sayOK
}
