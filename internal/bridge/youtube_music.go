package bridge

import (
	"context"
	"strings"
	"unicode"

	"github.com/skrashevich/quasar-bridge/internal/playback"
	"github.com/skrashevich/quasar-bridge/internal/reply"
)

// Fork: YouTube Music (ytm-alice on the speaker) is the only music channel.
// Engine.MusicRoute turns these rules on; without it nothing here applies.

const sayMusicUnclear = "Не удалось понять, что включить. Повтори, пожалуйста."

// idlePlayer answers some player words while nothing plays.
type idlePlayer interface {
	HandlesIdle(action string) bool
}

// classifierRoutes are the routes the classifiers may choose.
func (e *Engine) classifierRoutes() []string {
	routes := []string{"home", "llm", "yandex"}
	if e.MusicRoute {
		routes = append(routes, "music")
	}
	return routes
}

func (e *Engine) classifierRoute(route string) bool {
	return directRoute(route) || (e.MusicRoute && route == "music")
}

// classifierFailed keeps on_error, except that a launch word with YouTube
// configured gets a spoken retry request instead of Yandex Music.
func (e *Engine) classifierFailed(failed RouteDecision, text string) RouteDecision {
	if e.MusicRoute && launchWord(text) {
		return RouteDecision{Route: "music_unclear", Source: "classifier_error"}
	}
	return failed
}

func launchWord(text string) bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && r != '-' })
	if len(words) > 0 && words[0] == "алиса" {
		words = words[1:]
	}
	if len(words) < 2 {
		return false
	}
	switch words[0] {
	case "включи", "поставь", "запусти", "сыграй":
		return true
	}
	return false
}

// playerCommand names a player word. With YouTube it also knows «играй»,
// «что играет» and the like words, which ytm-alice cannot use.
func (e *Engine) playerCommand(text string) string {
	if action := playback.Command(text); action != "" || !e.MusicRoute {
		return action
	}
	s := strings.Trim(strings.ToLower(strings.ReplaceAll(text, "ё", "е")), " .,!?")
	s = strings.TrimSpace(strings.TrimPrefix(s, "алиса"))
	s = strings.Trim(s, " ,")
	switch s {
	case "играй", "играй дальше":
		return "resume"
	case "что играет", "что сейчас играет", "что это играет", "что за песня", "что это за песня", "какая это песня":
		return "what"
	case "лайк", "дизлайк", "поставь лайк", "поставь дизлайк":
		return "ignore"
	}
	return ""
}

// playerRoute reports a player word YouTube handles now. While nothing
// sounds and a Claude conversation is open, «продолжай», «дальше» belong to
// the conversation.
func (e *Engine) playerRoute(ctx context.Context, text string, conversation bool) bool {
	if e.Playback == nil {
		return false
	}
	action := e.playerCommand(text)
	if action == "" {
		return false
	}
	if e.Playback.Active(ctx) {
		return true
	}
	idle, ok := e.Playback.(idlePlayer)
	return e.MusicRoute && ok && !conversation && idle.HandlesIdle(action)
}

// routeTagFor tags YouTube replies [music] for the classifiers, matching the
// prompt's music route; everything else keeps routeTag.
func (e *Engine) routeTagFor(route string) string {
	if e.MusicRoute && (route == "music" || route == "playback" || route == "music_unclear") {
		return "music"
	}
	return routeTag(route)
}

// keepsDialogue: a YouTube command does not end the Claude conversation.
func (e *Engine) keepsDialogue(route string) bool {
	return e.MusicRoute && (route == "music" || route == "playback" || route == "music_unclear")
}

// quietOverMusic keeps the microphone closed after any reply while YouTube plays.
func (e *Engine) quietOverMusic(ctx context.Context) {
	if e.MusicRoute && e.Playback != nil && e.Playback.Active(ctx) {
		reply.SuppressListening(ctx)
	}
}
