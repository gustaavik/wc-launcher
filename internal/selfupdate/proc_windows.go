package selfupdate

import (
	"errors"
	"os/exec"

	"golang.org/x/sys/windows"
)

// detach is a no-op on Windows: a child process is not part of its parent's
// process group unless it is explicitly put there.
func detach(cmd *exec.Cmd) {}

// processAlive reports whether pid is still running.
//
// Not os.FindProcess: on Windows that opens a handle and never closes it, and
// an exited process stays openable for as long as any handle to it is open —
// so every pid read as alive and the helper always sat out the full parentWait.
// Instead the handle is waited on with a zero timeout, which asks the question
// directly, and closed again.
func processAlive(pid int) bool {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// Access denied means it exists but is somebody else's. As on Unix,
		// reading that as "gone" would start replacing a running launcher.
		// Anything else — ERROR_INVALID_PARAMETER, typically — means no such
		// process.
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(handle)

	event, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return true
	}
	return event == uint32(windows.WAIT_TIMEOUT)
}
