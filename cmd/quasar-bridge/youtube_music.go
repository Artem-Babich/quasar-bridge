package main

import (
	"context"
	"log/slog"
	"net"
	"net/url"

	"github.com/skrashevich/quasar-bridge/internal/bridge"
	"github.com/skrashevich/quasar-bridge/internal/sound"
	"github.com/skrashevich/quasar-bridge/internal/ytmusic"
)

// useYouTubeMusic makes ytm-alice the only music channel when its panel URL
// is set (fork). It replaces site music in the music and playback slots.
func useYouTubeMusic(engine *bridge.Engine, panelURL string) bool {
	if panelURL == "" {
		return false
	}
	u, err := url.Parse(panelURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		slog.Error("QUASAR_YTMUSIC_URL must be an http(s) panel address; YouTube Music is off")
		return false
	}
	// The panel answers only its own IP address or alice.lan/alice.local.
	if host := u.Hostname(); net.ParseIP(host) == nil && host != "alice.lan" && host != "alice.local" {
		slog.Error("QUASAR_YTMUSIC_URL must name the speaker by IP address; YouTube Music is off")
		return false
	}
	player := &ytmusic.Player{
		Client: &ytmusic.Client{BaseURL: panelURL},
		// A silent reply: audio_stop without speech. ytm-alice plays through
		// its own player, so the stop reaches only Yandex's player.
		Silence: func(ctx context.Context) error { return sound.Play(ctx, sound.Request{Command: "stop"}) },
	}
	engine.Music, engine.Playback, engine.MusicRoute = player, player, true
	slog.Info("youtube_music", "panel", u.Host)
	return true
}
