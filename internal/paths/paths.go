// Package paths resolves where the launcher keeps everything on disk.
//
// One root, four things under it:
//
//	<app-data>/Wyvencraft/
//	  launcher.json      launcher settings
//	  versions/<tag>/    an installed game build: the binary plus assets/
//	  data/              the game's WYVEN_DATA_DIR: saves, profile.toml, ...
//	  logs/              launcher.log, game.log
//	  launcher-update/   a downloaded launcher build, until it replaces this one
//
// The split between versions/ and data/ is the point. Applying an update
// replaces a whole version directory, so nothing that must survive one may live
// there. The game agrees with this layout: it resolves the same four runtime
// files against WYVEN_DATA_DIR, which the launcher always sets explicitly.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// AppDir is the directory name under the OS application-data root. It must
// match APP_DIR in the game's src/paths.rs, so that a game started outside the
// launcher finds the same data.
const AppDir = "Wyvencraft"

// Layout is the resolved set of directories. Build one with New and pass it
// down; nothing below should call New again.
type Layout struct {
	// Root is <app-data>/Wyvencraft.
	Root string
	// Versions holds one directory per installed build.
	Versions string
	// Data is handed to the game as WYVEN_DATA_DIR.
	Data string
	// Logs holds launcher.log and game.log.
	Logs string
}

// New resolves the layout and creates every directory in it.
//
// Directories are created up front rather than lazily: a permission problem
// should surface at startup, where it can be reported, and not at the moment a
// player clicks Play.
func New() (Layout, error) {
	root, err := appDataRoot()
	if err != nil {
		return Layout{}, err
	}
	root = filepath.Join(root, AppDir)

	layout := Layout{
		Root:     root,
		Versions: filepath.Join(root, "versions"),
		Data:     filepath.Join(root, "data"),
		Logs:     filepath.Join(root, "logs"),
	}

	for _, dir := range []string{layout.Root, layout.Versions, layout.Data, layout.Logs} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Layout{}, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return layout, nil
}

// SettingsFile is where launcher.json lives.
func (l Layout) SettingsFile() string { return filepath.Join(l.Root, "launcher.json") }

// StateFile records which version is installed.
func (l Layout) StateFile() string { return filepath.Join(l.Root, "installed.json") }

// VersionDir is the install directory for one release tag. It becomes the
// game's working directory, which is how the game finds assets/.
func (l Layout) VersionDir(tag string) string { return filepath.Join(l.Versions, safeTag(tag)) }

// ProfileFile is the game's profile.toml — how a session is handed over.
func (l Layout) ProfileFile() string { return filepath.Join(l.Data, "profile.toml") }

// KeysFile is the game's authkeys.toml, needed before anyone can host.
func (l Layout) KeysFile() string { return filepath.Join(l.Data, "authkeys.toml") }

// GameLog is where the child process's stderr is mirrored.
func (l Layout) GameLog() string { return filepath.Join(l.Logs, "game.log") }

// LauncherLog is the launcher's own log.
func (l Layout) LauncherLog() string { return filepath.Join(l.Logs, "launcher.log") }

// LauncherUpdateRoot holds a downloaded launcher build until it is swapped in.
//
// Deliberately outside versions/, which the game installer prunes: a staged
// launcher is not a game build and must not be deleted to make room for one.
func (l Layout) LauncherUpdateRoot() string { return filepath.Join(l.Root, "launcher-update") }

// LauncherUpdateDir is where one staged launcher build is unpacked. The tag is
// sanitised exactly as VersionDir sanitises it, and for the same reason: it
// comes from a remote server and would otherwise be a path traversal.
func (l Layout) LauncherUpdateDir(tag string) string {
	return filepath.Join(l.LauncherUpdateRoot(), safeTag(tag))
}

// appDataRoot is the OS application-data directory, without the AppDir suffix.
//
// Go has no stdlib equivalent of Rust's dirs::data_dir, and os.UserConfigDir
// disagrees with it on Linux (~/.config versus ~/.local/share). Spelled out
// here so the two implementations cannot drift: this must match app_data_root
// in the game's src/paths.rs.
func appDataRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support"), nil
	case "windows":
		// Roaming, matching dirs::data_dir. Falls back to the home directory
		// on the rare system where APPDATA is unset.
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return appdata, nil
		}
		return home, nil
	default:
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return xdg, nil
		}
		return filepath.Join(home, ".local", "share"), nil
	}
}

// safeTag reduces a release tag to something usable as a single directory name.
//
// Tags come from a remote server, and a tag of "../../etc" would otherwise turn
// VersionDir into a write primitive pointing anywhere on disk. Allowlisting is
// the only approach that holds here: blocklisting separators misses the ones
// the other OS uses.
func safeTag(tag string) string {
	cleaned := make([]rune, 0, len(tag))
	for _, r := range tag {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			cleaned = append(cleaned, r)
		case r == '-' || r == '_' || r == '+':
			cleaned = append(cleaned, r)
		case r == '.':
			// A dot belongs inside a version number and never at the front,
			// where it would make ".", ".." or a hidden directory.
			if len(cleaned) == 0 {
				cleaned = append(cleaned, '_')
			} else {
				cleaned = append(cleaned, r)
			}
		default:
			cleaned = append(cleaned, '_')
		}
	}
	if len(cleaned) == 0 {
		return "_"
	}
	return string(cleaned)
}
