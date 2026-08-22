// Package config holds the launcher's own settings, in launcher.json.
//
// Deliberately small. Anything the game owns lives in its data directory and is
// not duplicated here — in particular the session, which is stored once, in the
// profile.toml the game reads.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gustaavik/wc-launcher/internal/wcauth"
)

// Settings is the contents of launcher.json.
type Settings struct {
	// AuthURL is the account server. Empty means the shipped default.
	AuthURL string `json:"authUrl,omitempty"`
	// LogFilter overrides RUST_LOG for the game. Empty means the game's default.
	LogFilter string `json:"logFilter,omitempty"`
}

// Load reads settings, returning defaults when the file is missing.
//
// A corrupt file is also defaults rather than an error: the alternative is a
// launcher that will not start until someone edits JSON by hand, and nothing in
// here is worth that.
func Load(path string) Settings {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Settings{}
	}
	var settings Settings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return Settings{}
	}
	return settings
}

// Save writes settings.
func Save(path string, settings Settings) error {
	raw, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// ResolvedAuthURL is the account server to use, with the default filled in.
func (s Settings) ResolvedAuthURL() string {
	if url := strings.TrimSpace(s.AuthURL); url != "" {
		return url
	}
	return wcauth.DefaultURL
}
