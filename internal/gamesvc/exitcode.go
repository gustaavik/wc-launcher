package gamesvc

import "fmt"

// Windows NTSTATUS codes a process can end with before it logs anything. Go's
// ExitCode reports them as the unsigned value, 3221225781 for 0xC0000135.
const (
	statusAccessViolation   = 0xC0000005
	statusInvalidImage      = 0xC000007B
	statusDLLNotFound       = 0xC0000135
	statusEntryPointMissing = 0xC0000139
)

// exitMessage turns an exit code into what the player is told.
//
// A loader failure kills the game before main, so game.log is empty and the
// generic "see the log" would send the player to a blank file. Those codes get
// advice instead. The code itself is always kept, in hex for anything that is
// an NTSTATUS, because that is what a bug report needs.
func exitMessage(code int) string {
	if code == 0 {
		return "Wyvencraft closed."
	}

	const reinstall = "Wyvencraft could not start because a file it needs is missing or damaged. Try reinstalling the game."
	switch uint32(code) {
	case statusDLLNotFound, statusEntryPointMissing:
		return fmt.Sprintf("%s (A required DLL is missing: code 0x%08X.)", reinstall, uint32(code))
	case statusInvalidImage:
		return fmt.Sprintf("%s (Code 0x%08X.)", reinstall, uint32(code))
	case statusAccessViolation:
		return fmt.Sprintf("Wyvencraft crashed (code 0x%08X). See the log for details.", uint32(code))
	}

	if uint32(code)&0xC0000000 == 0xC0000000 {
		return fmt.Sprintf("Wyvencraft exited with code 0x%08X. See the log for details.", uint32(code))
	}
	return fmt.Sprintf("Wyvencraft exited with code %d. See the log for details.", code)
}
