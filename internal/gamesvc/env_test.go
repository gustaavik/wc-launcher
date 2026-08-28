package gamesvc

import (
	"strings"
	"testing"
)

func envMap(entries []string) map[string]string {
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			out[name] = value
		}
	}
	return out
}

// WYVEN_BOOT_INGAME, WYVEN_HOST and WYVEN_JOIN are read for *presence*:
// WYVEN_HOST=0 still enables hosting. A developer whose shell has one set would
// otherwise get a launcher that silently boots straight into a world.
func TestPresenceOnlyDevVariablesAreRemovedNotOverridden(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"WYVEN_BOOT_INGAME=",
		"WYVEN_HOST=0",
		"WYVEN_JOIN=127.0.0.1:6091",
		"WYVEN_WORLD=scratch",
		"WYVEN_USERNAME=someone",
		"WYVEN_PASSWORD=hunter2hunter2",
		"WYVEN_CLIENT_ID=99",
	}

	env := envMap(buildEnv(base, Options{DataDir: "/data"}, nil))

	for _, name := range []string{
		"WYVEN_BOOT_INGAME", "WYVEN_HOST", "WYVEN_JOIN", "WYVEN_WORLD",
		"WYVEN_USERNAME", "WYVEN_PASSWORD", "WYVEN_CLIENT_ID",
	} {
		if value, present := env[name]; present {
			t.Errorf("%s should be absent, got %q", name, value)
		}
	}
}

func TestTheGameIsToldWhereItsDataAndAuthServerAre(t *testing.T) {
	env := envMap(buildEnv([]string{"PATH=/usr/bin"}, Options{
		DataDir: "/data",
		AuthURL: "https://auth.example",
	}, nil))

	if env["WYVEN_DATA_DIR"] != "/data" {
		t.Errorf("WYVEN_DATA_DIR = %q", env["WYVEN_DATA_DIR"])
	}
	if env["WYVEN_AUTH_URL"] != "https://auth.example" {
		t.Errorf("WYVEN_AUTH_URL = %q", env["WYVEN_AUTH_URL"])
	}
	if env["RUST_LOG"] == "" {
		t.Error("RUST_LOG should have a default")
	}
	if env["PATH"] != "/usr/bin" {
		t.Error("the rest of the environment should be inherited")
	}
}

// Two entries for the same name is undefined behaviour in exec; the last-wins
// convention is not guaranteed across platforms.
func TestAnInheritedDataDirDoesNotSurviveAlongsideTheOneWeSet(t *testing.T) {
	entries := buildEnv([]string{
		"WYVEN_DATA_DIR=/stale",
		"WYVEN_AUTH_URL=http://stale",
		"RUST_LOG=trace",
	}, Options{DataDir: "/data", AuthURL: "https://fresh"}, nil)

	for _, name := range []string{"WYVEN_DATA_DIR", "WYVEN_AUTH_URL", "RUST_LOG"} {
		count := 0
		for _, entry := range entries {
			if strings.HasPrefix(entry, name+"=") {
				count++
			}
		}
		if count != 1 {
			t.Errorf("%s appears %d times, want exactly 1", name, count)
		}
	}

	env := envMap(entries)
	if env["WYVEN_DATA_DIR"] != "/data" || env["WYVEN_AUTH_URL"] != "https://fresh" {
		t.Errorf("stale values won: %v", env)
	}
}

func TestVulkanVariablesArePassedThrough(t *testing.T) {
	env := envMap(buildEnv(nil, Options{DataDir: "/data"}, map[string]string{
		"VK_ICD_FILENAMES":  "/opt/homebrew/etc/vulkan/icd.d/MoltenVK_icd.json",
		"DYLD_LIBRARY_PATH": "/opt/homebrew/lib",
	}))

	if env["VK_ICD_FILENAMES"] == "" || env["DYLD_LIBRARY_PATH"] == "" {
		t.Errorf("Vulkan variables missing: %v", env)
	}
}

func TestAnExplicitLogFilterWins(t *testing.T) {
	env := envMap(buildEnv(nil, Options{DataDir: "/d", LogFilter: "debug"}, nil))
	if env["RUST_LOG"] != "debug" {
		t.Errorf("RUST_LOG = %q, want debug", env["RUST_LOG"])
	}
}
