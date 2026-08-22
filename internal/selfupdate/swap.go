package selfupdate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ApplyFlag is the argument the launcher recognises to become an update
// helper instead of starting its UI. Handled at the very top of main.
const ApplyFlag = "--apply-update"

// parentWait bounds how long the helper waits for the launcher that spawned it
// to quit. Generous: quitting is normally instant, and giving up early would
// mean overwriting a bundle that is still executing from it.
const parentWait = 30 * time.Second

// Apply hands the swap to the staged build and returns.
//
// The replacement cannot be done by the process being replaced, so the new
// launcher does it: it is started with ApplyFlag, waits for this process to
// quit, puts itself in place, and relaunches. The caller must quit immediately
// after this returns.
func Apply(staged string, target Target) error {
	if !target.Writable {
		return fmt.Errorf("%s cannot be replaced from here; move the launcher somewhere you can write to, such as /Applications", target.Path)
	}

	helper := staged
	if strings.HasSuffix(staged, ".app") {
		exe, err := bundleExecutable(staged)
		if err != nil {
			return err
		}
		helper = exe
	}

	cmd := exec.Command(helper, ApplyFlag, target.Path, strconv.Itoa(os.Getpid()))
	// Its own session, so quitting the launcher — or force-quitting it — does
	// not take the helper down with it mid-swap.
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start the update helper: %w", err)
	}
	// Nothing waits on the helper: this process is about to exit, and the
	// helper deliberately outlives it to be reparented.
	return cmd.Process.Release()
}

// Swap is the helper side of Apply, run by the staged build.
//
// It waits for the launcher that spawned it to quit, replaces that launcher
// with itself, and starts the result.
func Swap(target, parentPID string) error {
	pid, err := strconv.Atoi(parentPID)
	if err != nil {
		return fmt.Errorf("bad parent process id %q", parentPID)
	}

	// Checked before the wait, not after: a swap that can never work should
	// say so now rather than in half a minute.
	staged, err := Locate()
	if err != nil {
		return err
	}
	if staged.Path == target {
		return fmt.Errorf("refusing to replace %s with itself", target)
	}

	waitForExit(pid, parentWait)

	if err := replace(staged.Path, target); err != nil {
		return err
	}
	return relaunch(target)
}

// replace puts src in place of dst without a window where dst does not exist.
//
// The copy lands beside dst first, so a failed or partial copy leaves the
// installed launcher untouched; only then are the two swapped by rename, and a
// failed swap puts the original back.
func replace(src, dst string) error {
	staging := dst + ".new"
	previous := dst + ".old"

	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("clear %s: %w", staging, err)
	}
	if err := copyTree(src, staging); err != nil {
		return err
	}
	if err := os.RemoveAll(previous); err != nil {
		return fmt.Errorf("clear %s: %w", previous, err)
	}

	replacing := false
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, previous); err != nil {
			return fmt.Errorf("move the old launcher aside: %w", err)
		}
		replacing = true
	}

	if err := os.Rename(staging, dst); err != nil {
		if replacing {
			// Put the working launcher back rather than leaving nothing there.
			_ = os.Rename(previous, dst)
		}
		return fmt.Errorf("move the new launcher into place: %w", err)
	}

	os.RemoveAll(previous)
	return nil
}

// copyTree copies a bundle or a binary, preserving everything that matters.
//
// ditto on macOS, for the same reason unpack uses it: a code signature only
// survives an exact reproduction, extended attributes and the stapled
// notarization ticket included.
func copyTree(src, dst string) error {
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("/usr/bin/ditto", src, dst).CombinedOutput(); err != nil {
			return fmt.Errorf("copy the new launcher: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return fmt.Errorf("applying a launcher update is not supported on %s", runtime.GOOS)
}

// relaunch starts the freshly installed launcher.
func relaunch(target string) error {
	if runtime.GOOS == "darwin" && strings.HasSuffix(target, ".app") {
		// open, not the binary directly: LaunchServices registers the bundle
		// and gives it a Dock icon and an activation, which exec'ing the
		// Mach-O does not.
		cmd := exec.Command("/usr/bin/open", "-n", target)
		detach(cmd)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start the updated launcher: %w", err)
		}
		return nil
	}

	cmd := exec.Command(target)
	cmd.Dir = filepath.Dir(target)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start the updated launcher: %w", err)
	}
	return nil
}

// waitForExit blocks until pid is gone, or until the deadline passes.
func waitForExit(pid int, within time.Duration) {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
