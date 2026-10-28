package bridge

// User story: Decisions never gets more time than the whole classifier step,
// whatever decisions_timeout_ms says.

import (
	"testing"
	"time"
)

func TestDecisionsTimeoutIsCappedAtTimeoutMs(t *testing.T) {
	for _, c := range []ClassifierConfig{{TimeoutMS: 1000, DecisionsTimeoutMS: 2000}, {TimeoutMS: 1000}} {
		if got := c.DecisionsTimeout(); got != time.Second {
			t.Fatalf("%+v: DecisionsTimeout()=%v, want 1s", c, got)
		}
	}
	if got := (ClassifierConfig{TimeoutMS: 8000, DecisionsTimeoutMS: 300}).DecisionsTimeout(); got != 300*time.Millisecond {
		t.Fatalf("DecisionsTimeout()=%v, want 300ms", got)
	}
}
