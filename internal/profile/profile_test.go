package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempFile(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

func TestAMissingProfileIsAFreshInstallNotAnError(t *testing.T) {
	p, err := Read(tempFile(t, "profile.toml"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if p.ClientID != "" || p.Account != nil {
		t.Errorf("want a zero profile, got %+v", p)
	}
}

// The client_id keys singleplayer saves made while signed out. Losing it on
// sign-in would orphan them, which is why the game does a read-modify-write and
// why the launcher must too.
func TestStoringAnAccountPreservesAnExistingClientID(t *testing.T) {
	path := tempFile(t, "profile.toml")
	if err := os.WriteFile(path, []byte("client_id = \"1787389778214353360\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := StoreAccount(path, &Account{
		AccountID:    "67757374-6176-0000-0000-000000000000",
		Username:     "gustav",
		RefreshToken: "rt-1",
	})
	if err != nil {
		t.Fatalf("StoreAccount: %v", err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.ClientID != "1787389778214353360" {
		t.Errorf("client_id = %q, want it preserved", got.ClientID)
	}
	if got.Account == nil || got.Account.Username != "gustav" || got.Account.RefreshToken != "rt-1" {
		t.Errorf("account = %+v", got.Account)
	}
}

func TestSigningOutClearsTheAccountAndKeepsTheClientID(t *testing.T) {
	path := tempFile(t, "profile.toml")
	if err := Write(path, Profile{
		ClientID: "42",
		Account:  &Account{AccountID: "a", Username: "gustav", RefreshToken: "rt"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := StoreAccount(path, nil); err != nil {
		t.Fatalf("StoreAccount(nil): %v", err)
	}

	got, _ := Read(path)
	if got.Account != nil {
		t.Errorf("account should be gone, got %+v", got.Account)
	}
	if got.ClientID != "42" {
		t.Errorf("client_id = %q, want 42", got.ClientID)
	}
}

// The exact bytes the game writes, so a profile written by one is read by the
// other. Reproduced from a real profile.toml.
func TestReadsTheProfileTheGameWrites(t *testing.T) {
	path := tempFile(t, "profile.toml")
	written := `client_id = "1787389778214353360"

[account]
account_id = "67757374-6176-0000-0000-000000000000"
username = "gustav"
refresh_token = "refresh-for-gustav"
`
	if err := os.WriteFile(path, []byte(written), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.ClientID != "1787389778214353360" {
		t.Errorf("client_id = %q", got.ClientID)
	}
	if got.Account == nil {
		t.Fatal("account table not read")
	}
	if got.Account.AccountID != "67757374-6176-0000-0000-000000000000" {
		t.Errorf("account_id = %q", got.Account.AccountID)
	}
	if got.Account.RefreshToken != "refresh-for-gustav" {
		t.Errorf("refresh_token = %q", got.Account.RefreshToken)
	}
}

func TestAProfileRoundTripsThroughWriteAndRead(t *testing.T) {
	path := tempFile(t, "profile.toml")
	want := Profile{
		ClientID: "1787389778214353360",
		Account:  &Account{AccountID: "id", Username: "gustav", RefreshToken: "rt"},
	}
	if err := Write(path, want); err != nil {
		t.Fatal(err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ClientID != want.ClientID || *got.Account != *want.Account {
		t.Errorf("round trip lost data: %+v", got)
	}
}

// The refresh token is the sensitive part of the file.
func TestTheProfileIsNotWorldReadable(t *testing.T) {
	path := tempFile(t, "profile.toml")
	if err := Write(path, Profile{ClientID: "1"}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

func TestWritingIsAtomicAndLeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.toml")
	if err := Write(path, Profile{ClientID: "1"}); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("left a temp file behind: %s", entry.Name())
		}
	}
}

func TestKeysAreWrittenInTheShapeTheGameParses(t *testing.T) {
	path := tempFile(t, "authkeys.toml")
	err := WriteKeys(path, []Key{
		{ID: 0, PublicKey: "+YXA0P5bXFoVBD24V0hVOXYS9dPp81tX9BfLjA2tL+k="},
		{ID: 1, PublicKey: "GX9rI+FshTLGq8g4+s1ep4m+DHaykgM0A5v6iz02jWE="},
	})
	if err != nil {
		t.Fatalf("WriteKeys: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Count(text, "[[keys]]") != 2 {
		t.Errorf("want two key tables, got:\n%s", text)
	}
	if !strings.Contains(text, "id = 0") || !strings.Contains(text, `public_key = "+YXA0P5bXFoVBD24V0hVOXYS9dPp81tX9BfLjA2tL+k="`) {
		t.Errorf("unexpected content:\n%s", text)
	}
}

// An empty key set makes a host refuse every join. Writing one over a good file
// would break hosting that would otherwise have kept working.
func TestWritingAnEmptyKeySetIsRefused(t *testing.T) {
	path := tempFile(t, "authkeys.toml")
	if err := WriteKeys(path, nil); err == nil {
		t.Fatal("want an error for an empty key set")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("no file should have been written")
	}
}

func TestQuotingSurvivesAwkwardCharacters(t *testing.T) {
	path := tempFile(t, "profile.toml")
	// Not expected in a real token, but a parser that breaks on one would
	// corrupt the file rather than fail loudly.
	awkward := `tok"en\with`
	if err := Write(path, Profile{
		ClientID: "1",
		Account:  &Account{AccountID: "id", Username: "gustav", RefreshToken: awkward},
	}); err != nil {
		t.Fatal(err)
	}

	got, _ := Read(path)
	if got.Account == nil || got.Account.RefreshToken != awkward {
		t.Errorf("refresh_token = %q, want %q", got.Account.RefreshToken, awkward)
	}
}

// A table that exists but is missing the token is not a session.
func TestAnIncompleteAccountTableIsTreatedAsSignedOut(t *testing.T) {
	path := tempFile(t, "profile.toml")
	if err := os.WriteFile(path, []byte("client_id = \"1\"\n\n[account]\nusername = \"gustav\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, _ := Read(path)
	if got.Account != nil {
		t.Errorf("want no account, got %+v", got.Account)
	}
}

// The game cannot read a profile whose client_id is not a u64, and older builds
// responded by rewriting the file with no [account] table — destroying the very
// session StoreAccount exists to hand over.
func TestStoringAnAccountOnAFreshInstallMintsAUsableClientID(t *testing.T) {
	path := tempFile(t, "profile.toml")

	err := StoreAccount(path, &Account{AccountID: "id", Username: "gustav", RefreshToken: "rt"})
	if err != nil {
		t.Fatalf("StoreAccount: %v", err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ClientID == "" {
		t.Fatal("client_id was left empty")
	}
	if !validClientID(got.ClientID) {
		t.Errorf("client_id = %q, which the game cannot parse", got.ClientID)
	}
}

func TestAnUnusableClientIDIsReplacedRatherThanPreserved(t *testing.T) {
	path := tempFile(t, "profile.toml")
	// A file left behind by an earlier launcher that wrote an empty id.
	if err := os.WriteFile(path, []byte("client_id = \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := StoreAccount(path, &Account{AccountID: "id", Username: "g", RefreshToken: "rt"}); err != nil {
		t.Fatal(err)
	}

	got, _ := Read(path)
	if !validClientID(got.ClientID) {
		t.Errorf("client_id = %q, want a usable one", got.ClientID)
	}
}

func TestValidClientIDMatchesWhatAU64CanHold(t *testing.T) {
	for _, ok := range []string{"1", "1787389778214353360", "18446744073709551615"} {
		if !validClientID(ok) {
			t.Errorf("validClientID(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "   ", "abc", "-1", "1.5", "18446744073709551616", "99999999999999999999999"} {
		if validClientID(bad) {
			t.Errorf("validClientID(%q) = true, want false", bad)
		}
	}
}

func TestMintedClientIDsAreDistinctAndNonZero(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		id := mintClientID()
		if id == "0" {
			t.Fatal("zero is reserved for the host's local player")
		}
		if !validClientID(id) {
			t.Fatalf("minted an unusable id %q", id)
		}
		seen[id] = true
	}
	if len(seen) < 50 {
		t.Errorf("only %d distinct ids from 50 mints", len(seen))
	}
}
