package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Launching the real game through the real launch path. Skipped unless pointed
// at a game build:
//
//	WC_IT_AUTH_URL=http://localhost:8080 \
//	WC_IT_USERNAME=... WC_IT_PASSWORD=... \
//	WCL_DEV_GAME_DIR=/tmp/wc-devgame \
//	go test ./internal/services/ -run TestLaunch -v
//
// The thing being checked is not that the game works — it has its own tests —
// but that the launcher hands it the right working directory, the right data
// directory, and a session it can restore.
func TestLaunchHandsTheGameASessionAndADataDirectory(t *testing.T) {
	authURL, username, password := integrationEnv(t)
	gameDir := os.Getenv("WCL_DEV_GAME_DIR")
	if gameDir == "" {
		t.Skip("set WCL_DEV_GAME_DIR to a directory holding a wyvencraft binary and assets/")
	}

	core := testCore(t, authURL)
	auth := NewAuthService(core)
	game := NewGameService(core)

	if result := auth.Login(username, password); result.Error != "" {
		t.Fatalf("login: %s", result.Error)
	}

	if msg := game.Launch(); msg != "" {
		t.Fatalf("launch: %s", msg)
	}
	t.Cleanup(func() { _ = core.Runner.Stop() })

	// Give the game long enough to resolve its paths, restore the session and
	// open a window.
	deadline := time.Now().Add(20 * time.Second)
	var log string
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(core.Layout.GameLog())
		if err == nil {
			log = string(raw)
			if strings.Contains(log, "game data in") && strings.Contains(log, "session") {
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}

	if !core.Runner.Running() {
		t.Fatalf("the game exited immediately. Log:\n%s", log)
	}

	// 1. It resolved the data directory the launcher gave it, not its own
	//    default and not the working directory.
	if !strings.Contains(log, "game data in "+core.Layout.Data) {
		t.Errorf("game did not use the launcher's data dir.\nWanted %q in:\n%s", core.Layout.Data, log)
	}
	if !strings.Contains(log, "from WYVEN_DATA_DIR") {
		t.Errorf("the data dir did not come from WYVEN_DATA_DIR:\n%s", log)
	}

	// 2. It restored the session the launcher wrote, rather than showing a
	//    login screen (there is none) or booting offline.
	if !strings.Contains(log, "restored session for "+username) {
		t.Errorf("game did not restore the handed-over session:\n%s", log)
	}
	if strings.Contains(log, "playing offline") {
		t.Errorf("game fell back to offline despite a valid session:\n%s", log)
	}

	// 3. It found its assets, which only works if the working directory is the
	//    version directory.
	if strings.Contains(log, "no assets/blocks.toml") {
		t.Errorf("game could not find its assets — wrong working directory:\n%s", log)
	}

	// 4. Rotation happened and was persisted: the game refreshes on restore, so
	//    the token on disk is not the one the launcher wrote.
	if _, err := os.Stat(filepath.Join(core.Layout.Data, "profile.toml")); err != nil {
		t.Errorf("profile.toml missing from the data dir: %v", err)
	}

	if err := core.Runner.Stop(); err != nil {
		t.Errorf("stop: %v", err)
	}
}
