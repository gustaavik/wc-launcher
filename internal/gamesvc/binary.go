package gamesvc

import "runtime"

// gameBinaryName is the executable inside an unpacked build. Kept here rather
// than imported from install so that gamesvc does not depend on it.
func gameBinaryName() string {
	if runtime.GOOS == "windows" {
		return "wyvencraft.exe"
	}
	return "wyvencraft"
}
