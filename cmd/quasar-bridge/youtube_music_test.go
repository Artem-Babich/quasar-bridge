package main

// User story: setting QUASAR_YTMUSIC_URL on Pi 3 is the switch that makes
// YouTube Music the only music; without it nothing changes, and a malformed
// address leaves YouTube off instead of breaking startup. The panel answers
// only its own IP address, so a host name is refused as well.

import (
	"testing"

	"github.com/skrashevich/quasar-bridge/internal/bridge"
	"github.com/skrashevich/quasar-bridge/internal/ytmusic"
)

func TestPanelURLMakesYouTubeTheMusicChannel(t *testing.T) {
	e := &bridge.Engine{}
	if !useYouTubeMusic(e, "http://192.0.2.20:24721") || !e.MusicRoute {
		t.Fatal("YouTube Music is not on")
	}
	if _, ok := e.Music.(*ytmusic.Player); !ok {
		t.Fatalf("music slot holds %T", e.Music)
	}
	if _, ok := e.Playback.(*ytmusic.Player); !ok {
		t.Fatalf("playback slot holds %T", e.Playback)
	}
}

func TestWithoutPanelURLMusicStaysAsBefore(t *testing.T) {
	for _, raw := range []string{"", "192.0.2.20:24721", "ftp://x", "http://speaker.example.invalid:24721"} {
		e := &bridge.Engine{}
		if useYouTubeMusic(e, raw) || e.MusicRoute || e.Music != nil || e.Playback != nil {
			t.Fatalf("%q turned YouTube on", raw)
		}
	}
}
