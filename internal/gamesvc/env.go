// Package gamesvc builds the environment for the game process, starts it, and
// watches it.
package gamesvc

import (
	"os"
	"strings"
)

// strippedVars are removed from the inherited environment before the game
// starts.
//
// The first three are read for *presence*, not value: WYVEN_HOST=0 still
// enables hosting. So they cannot be neutralised by setting them to something
// falsy — they must be absent. The rest would quietly override the session the
// launcher just handed over, or the identity a save is keyed on.
var strippedVars = []string{
	"WYVEN_BOOT_INGAME",
	"WYVEN_HOST",
	"WYVEN_JOIN",
	"WYVEN_WORLD",
	"WYVEN_SEED",
	"WYVEN_MODE",
	"WYVEN_USERNAME",
	"WYVEN_PASSWORD",
	"WYVEN_CLIENT_ID",
	"WYVEN_DEBUG_SPAWN",
}

// Options is everything that varies between launches.
type Options struct {
	// DataDir becomes WYVEN_DATA_DIR: where saves, profile.toml, ops.toml and
	// authkeys.toml live.
	DataDir string
	// AuthURL becomes WYVEN_AUTH_URL.
	AuthURL string
	// VersionDir is the install being launched. It becomes the working
	// directory, which is how the game finds assets/, and is searched for a
	// bundled Vulkan driver.
	VersionDir string
	// LogFilter becomes RUST_LOG. Empty means the game's own default.
	LogFilter string
	// MoltenVKDir is where the launcher installed the Vulkan driver, if it
	// has. Empty leaves the probe to whatever else is on the machine.
	MoltenVKDir string
}

// buildEnv returns the child's complete environment.
//
// Built from the launcher's own, minus the variables above, plus the ones the
// game needs. Starting from a copy rather than from nothing keeps PATH, HOME
// and the display/session variables that a windowed process needs.
func buildEnv(base []string, opts Options, vulkan map[string]string) []string {
	stripped := make(map[string]bool, len(strippedVars))
	for _, name := range strippedVars {
		stripped[name] = true
	}

	env := make([]string, 0, len(base)+8)
	for _, entry := range base {
		name, _, ok := strings.Cut(entry, "=")
		if ok && stripped[name] {
			continue
		}
		// Set explicitly below; drop any inherited copy so there is exactly one.
		if name == "WYVEN_DATA_DIR" || name == "WYVEN_AUTH_URL" || name == "RUST_LOG" {
			continue
		}
		env = append(env, entry)
	}

	env = append(env, "WYVEN_DATA_DIR="+opts.DataDir)
	if opts.AuthURL != "" {
		env = append(env, "WYVEN_AUTH_URL="+opts.AuthURL)
	}
	filter := opts.LogFilter
	if filter == "" {
		filter = "info,wyvencraft=info"
	}
	env = append(env, "RUST_LOG="+filter)

	for name, value := range vulkan {
		env = append(env, name+"="+value)
	}
	return env
}

// Environ is the launcher's own environment. Split out so tests can pass a
// fixed one instead of the real process environment, which is global state.
func Environ() []string { return os.Environ() }
