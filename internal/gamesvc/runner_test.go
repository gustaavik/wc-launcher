package gamesvc

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// standIn writes a shell-script "game" into a fresh version directory and
// fakes the Vulkan driver the way probeVulkan honours: an existing file named
// by VK_ICD_FILENAMES. Nothing renders here.
func standIn(t *testing.T, script string) Options {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	icd := filepath.Join(t.TempDir(), "icd.json")
	if err := os.WriteFile(icd, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VK_ICD_FILENAMES", icd)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, gameBinaryName()), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return Options{VersionDir: dir, DataDir: t.TempDir()}
}

// A game that dies at once — a missing DLL on Windows kills it before main —
// used to leave the launcher showing "Running": the exit was reported first
// and the start after it. The start must always be reported first.
func TestAGameThatExitsAtOnceIsReportedStartedThenExited(t *testing.T) {
	opts := standIn(t, "#!/bin/sh\nexit 3\n")

	var (
		mu     sync.Mutex
		states []Status
		done   = make(chan struct{})
	)
	runner := NewRunner()
	err := runner.Start(opts, filepath.Join(t.TempDir(), "game.log"), nil, func(s Status) {
		mu.Lock()
		states = append(states, s)
		mu.Unlock()
		if !s.Running {
			close(done)
		}
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the exit was never reported")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(states) != 2 || !states[0].Running || states[1].Running {
		t.Fatalf("states = %+v, want exactly [running, exited]", states)
	}
	if states[1].ExitCode != 3 {
		t.Errorf("exit code = %d, want 3", states[1].ExitCode)
	}
	if runner.Running() {
		t.Error("the runner still reports a game running after it exited")
	}
}
