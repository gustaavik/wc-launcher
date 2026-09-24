package services

import (
	"sync"
	"testing"

	"github.com/gustaavik/wc-launcher/internal/gamesvc"
)

type recordingEmitter struct {
	mu     sync.Mutex
	events []string
	data   []any
}

func (r *recordingEmitter) Emit(name string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, name)
	r.data = append(r.data, data)
}

// A UI left showing "Running" after the game had already gone must be able to
// recover through Stop: with nothing to kill, Stop reports the real state.
func TestStopWithNothingRunningReportsTheCurrentState(t *testing.T) {
	core := hermeticCore(t)
	emitter := &recordingEmitter{}
	core.Emitter = emitter

	if msg := NewGameService(core).Stop(); msg != "" {
		t.Fatalf("Stop = %q, want no error", msg)
	}

	emitter.mu.Lock()
	defer emitter.mu.Unlock()
	if len(emitter.events) != 1 || emitter.events[0] != "game:state" {
		t.Fatalf("events = %v, want one game:state", emitter.events)
	}
	if status, ok := emitter.data[0].(gamesvc.Status); !ok || status.Running {
		t.Errorf("emitted %+v, want a not-running status", emitter.data[0])
	}
}
