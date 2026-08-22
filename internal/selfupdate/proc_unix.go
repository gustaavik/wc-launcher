//go:build !windows

package selfupdate

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// detach puts the child in its own session, so a signal sent to the launcher's
// process group does not reach it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// processAlive reports whether pid still exists.
//
// Signal 0 performs the permission and existence checks and delivers nothing,
// which is exactly the question being asked.
func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// EPERM means the process is there but belongs to somebody else. The helper
	// only ever waits on the launcher that spawned it, so this should not
	// arise — but reading it as "gone" would start replacing a bundle that is
	// still executing.
	return errors.Is(err, syscall.EPERM)
}
