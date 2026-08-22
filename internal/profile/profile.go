// Package profile reads and writes the two files the launcher hands the game.
//
//   - profile.toml carries the session. Writing it is how a launcher signs the
//     player in: the game finds the [account] table at boot, refreshes it, and
//     goes straight to the menu.
//   - authkeys.toml carries the public keys a host verifies join tickets with.
//     A host without it refuses every join, and the game only fetches keys when
//     it establishes a session — so the launcher writes it too.
//
// Both are TOML the game already parses, so this writes them by hand rather
// than pulling in an encoder for two fixed shapes. Both are written atomically,
// as the game does, because a half-written profile.toml is an unreadable one
// and would look to the player like being signed out.
package profile

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Account is the [account] table of profile.toml.
type Account struct {
	AccountID    string
	Username     string
	RefreshToken string
}

// Profile is the whole of profile.toml.
type Profile struct {
	// ClientID is the machine-local identity, minted by the game on first run
	// and used for singleplayer saves made while signed out. Preserved across
	// writes: regenerating it orphans those saves.
	ClientID string
	// Account is absent when nobody is signed in.
	Account *Account
}

// Read parses profile.toml.
//
// A missing or unreadable file is not an error — it is a fresh install, and the
// game creates one itself on first run. Callers get a zero Profile.
func Read(path string) (Profile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Profile{}, nil
		}
		return Profile{}, fmt.Errorf("read %s: %w", path, err)
	}
	return parse(string(raw)), nil
}

// StoreAccount records (or with nil, forgets) the signed-in account, keeping
// whatever client_id the file already had.
//
// This is the launcher's half of the session handoff. It is also the step that
// must not be skipped or deferred after a refresh: rotation is single-use, so
// the pair received has to be on disk before anything else can fail.
func StoreAccount(path string, account *Account) error {
	existing, err := Read(path)
	if err != nil {
		// Unreadable rather than absent. Overwriting would silently discard a
		// client_id, so say so instead.
		return err
	}
	existing.Account = account

	// The game requires client_id to be present and parseable as a u64. If it
	// is neither, the game regenerates it — and older builds rewrote the file
	// with no [account] table while doing so, destroying the session this
	// function exists to hand over. So one is minted here rather than left
	// blank on a fresh install.
	if !validClientID(existing.ClientID) {
		existing.ClientID = mintClientID()
	}
	return Write(path, existing)
}

// validClientID reports whether the game will be able to read this id.
//
// Mirrors the game's own check: a decimal u64, trimmed.
func validClientID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 20 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	// Leading zeros parse fine; a value above u64 does not, and 20 digits is
	// the only length where that is possible.
	if len(id) == 20 && id > "18446744073709551615" {
		return false
	}
	return true
}

// mintClientID produces the machine-local identity the game keys signed-out
// singleplayer saves on.
//
// Random rather than derived from anything: it is not an account, and two
// installs on one machine should not collide. Never zero, matching the game,
// where zero is reserved for a host's own local player.
func mintClientID() string {
	var buf [8]byte
	if _, err := cryptorand.Read(buf[:]); err != nil {
		// crypto/rand does not fail in practice; if it somehow does, any
		// non-zero id is better than an unreadable file.
		return "1"
	}
	id := binary.LittleEndian.Uint64(buf[:])
	if id == 0 {
		id = 1
	}
	return fmt.Sprintf("%d", id)
}

// Write renders a profile and replaces the file atomically.
func Write(path string, p Profile) error {
	var b strings.Builder
	// The game stores this as a decimal string, not a TOML integer: it is a
	// u64, and TOML integers are signed 64-bit.
	fmt.Fprintf(&b, "client_id = %s\n", quote(p.ClientID))
	if p.Account != nil {
		b.WriteString("\n[account]\n")
		fmt.Fprintf(&b, "account_id = %s\n", quote(p.Account.AccountID))
		fmt.Fprintf(&b, "username = %s\n", quote(p.Account.Username))
		fmt.Fprintf(&b, "refresh_token = %s\n", quote(p.Account.RefreshToken))
	}
	return writeAtomic(path, []byte(b.String()), 0o600)
}

// Key is one entry of authkeys.toml.
type Key struct {
	ID int
	// PublicKey is base64 exactly as GET /api/v1/keys returned it, so the two
	// can be compared by eye when a join is being refused.
	PublicKey string
}

// WriteKeys replaces authkeys.toml with keys.
//
// Refuses to write an empty set. An empty file parses to an empty key set, and
// an empty key set makes a host refuse every join — so writing one would
// actively break hosting that a stale-but-valid file would have allowed.
func WriteKeys(path string, keys []Key) error {
	if len(keys) == 0 {
		return fmt.Errorf("refusing to write an empty %s: a host with no keys refuses every join",
			filepath.Base(path))
	}

	var b strings.Builder
	for _, key := range keys {
		b.WriteString("[[keys]]\n")
		fmt.Fprintf(&b, "id = %d\n", key.ID)
		fmt.Fprintf(&b, "public_key = %s\n\n", quote(key.PublicKey))
	}
	// Public keys. Not secret, unlike profile.toml.
	return writeAtomic(path, []byte(b.String()), 0o644)
}

// parse reads the handful of keys that matter, ignoring everything else.
//
// A real TOML parser would be more correct, but this file has one shape, is
// written by the two programs that read it, and the failure mode of guessing
// wrong is bounded: an unrecognised client_id is treated as absent, and the
// game mints a new one.
func parse(text string) Profile {
	var p Profile
	inAccount := false
	account := Account{}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inAccount = line == "[account]"
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = unquote(strings.TrimSpace(value))

		if !inAccount {
			if key == "client_id" {
				p.ClientID = value
			}
			continue
		}
		switch key {
		case "account_id":
			account.AccountID = value
		case "username":
			account.Username = value
		case "refresh_token":
			account.RefreshToken = value
		}
	}

	// A table with no usable account is the same as no table.
	if account.AccountID != "" && account.RefreshToken != "" {
		p.Account = &account
	}
	return p
}

func quote(value string) string {
	// TOML basic strings escape the same two characters JSON does; nothing here
	// can contain a control character.
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

func unquote(value string) string {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
		value = strings.ReplaceAll(value, `\"`, `"`)
		value = strings.ReplaceAll(value, `\\`, `\`)
	}
	return value
}

// writeAtomic writes via a temp file in the same directory, then renames.
//
// Same approach the game uses, and for the same reason: a torn profile.toml
// reads as "signed out", and the refresh token it held is not recoverable.
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once the rename succeeds

	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod %s: %w", name, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", name, err)
	}
	// Flushed before the rename: on a crash, rename-without-fsync can leave the
	// new name pointing at empty content.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
