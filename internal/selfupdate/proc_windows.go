package selfupdate

import (
	"os"
	"os/exec"
)

// detach is a no-op on Windows: a child process is not part of its parent's
// process group unless it is explicitly put there.
func detach(cmd *exec.Cmd) {}

// processAlive reports whether pid still exists. FindProcess opens a handle on
// Windows and fails once the process is gone, so the lookup is the answer.
func processAlive(pid int) bool {
	_, err := os.FindProcess(pid)
	return err == nil
}
