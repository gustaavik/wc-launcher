package selfupdate

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func writeBundle(t *testing.T, path, marker string) {
	t.Helper()
	macos := filepath.Join(path, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(macos, "Wyvencraft"), []byte(marker), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func markerOf(t *testing.T, bundle string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(bundle, "Contents", "MacOS", "Wyvencraft"))
	if err != nil {
		t.Fatalf("read %s: %v", bundle, err)
	}
	return string(raw)
}

func TestReplaceSwapsTheBundleAndLeavesNothingBehind(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skipf("copyTree uses ditto, which is macOS only")
	}
	dir := t.TempDir()

	installed := filepath.Join(dir, "Wyvencraft.app")
	staged := filepath.Join(dir, "staged", "Wyvencraft.app")
	writeBundle(t, installed, "old")
	writeBundle(t, staged, "new")

	if err := replace(staged, installed); err != nil {
		t.Fatalf("replace: %v", err)
	}

	if got := markerOf(t, installed); got != "new" {
		t.Errorf("installed bundle contains %q, want %q", got, "new")
	}
	// A leftover .old is tens of megabytes next to the app, and a leftover .new
	// would be picked up as a second copy of the launcher by Spotlight.
	for _, leftover := range []string{installed + ".old", installed + ".new"} {
		if _, err := os.Stat(leftover); err == nil {
			t.Errorf("%s was left behind", leftover)
		}
	}
	// The staged copy survives: it is cleaned up on the next normal start, so a
	// swap that goes wrong still has something to recover from.
	if got := markerOf(t, staged); got != "new" {
		t.Errorf("staged bundle contains %q, want it untouched", got)
	}
}

func TestAFailedCopyLeavesTheInstalledLauncherAlone(t *testing.T) {
	dir := t.TempDir()

	installed := filepath.Join(dir, "Wyvencraft.app")
	writeBundle(t, installed, "old")

	// The copy lands beside the target before anything is moved, so a source
	// that cannot be read never gets as far as touching the install.
	if err := replace(filepath.Join(dir, "does-not-exist.app"), installed); err == nil {
		t.Fatal("replace accepted a source that does not exist")
	}

	if got := markerOf(t, installed); got != "old" {
		t.Errorf("installed bundle contains %q, want it untouched", got)
	}
	if _, err := os.Stat(installed + ".old"); err == nil {
		t.Error("the installed launcher was moved aside before the copy succeeded")
	}
}

func TestReplaceInstallsWhereNothingIsYet(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skipf("copyTree uses ditto, which is macOS only")
	}
	dir := t.TempDir()

	staged := filepath.Join(dir, "staged", "Wyvencraft.app")
	writeBundle(t, staged, "new")

	target := filepath.Join(dir, "Wyvencraft.app")
	if err := replace(staged, target); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if got := markerOf(t, target); got != "new" {
		t.Errorf("installed bundle contains %q, want %q", got, "new")
	}
}

func TestSwapRefusesToReplaceTheStagedBuildWithItself(t *testing.T) {
	// Locate() resolves the running test binary, so pointing the swap at it is
	// the one self-replacement this can provoke. Left unguarded it would delete
	// the staged build and then copy the hole over the install.
	self, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	// This process is alive, so reaching the parent-exit wait would hang the
	// test: the refusal has to come before it.
	done := make(chan error, 1)
	go func() { done <- Swap(self.Path, strconv.Itoa(os.Getpid())) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Swap accepted a target identical to the staged build")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Swap waited for the parent before checking what it was replacing")
	}
}

func TestWaitForExitGivesUpRatherThanHanging(t *testing.T) {
	// The launcher that spawned the helper is normally gone in milliseconds. A
	// helper that waited forever on one that is not would leave a process that
	// never finishes the update and never exits.
	start := time.Now()
	waitForExit(os.Getpid(), 200*time.Millisecond)
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Errorf("returned after %v, want it to have waited out the deadline", elapsed)
	}
}

func TestProcessAliveKnowsThisProcess(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Error("this process reported as gone")
	}
}
