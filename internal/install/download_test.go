package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyAcceptsAMatchingHashAndRejectsEverythingElse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blob")
	content := []byte("the game, allegedly")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	good := hex.EncodeToString(sum[:])

	if err := Verify(path, good, nil); err != nil {
		t.Errorf("a matching hash should verify: %v", err)
	}
	if err := Verify(path, "00"+good[2:], nil); err == nil {
		t.Error("a mismatched hash should fail")
	}
}

// Skipping verification when no digest was published is how an unverified
// binary gets executed. It must be an error, not a pass.
func TestVerifyRefusesToPassWithNoExpectedHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blob")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Verify(path, "", nil); err == nil {
		t.Fatal("an empty expected hash must not verify")
	}
}

func TestDownloadWritesTheWholeBodyAndReportsProgress(t *testing.T) {
	body := make([]byte, 300*1024)
	for i := range body {
		body[i] = byte(i)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "archive.tar.gz", zeroTime, newReadSeeker(body))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "out.part")
	var last Progress
	err := Fetch(context.Background(), server.URL, path, int64(len(body)), func(p Progress) {
		last = p
	})
	if err != nil {
		t.Fatalf("download: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(body) {
		t.Fatalf("wrote %d bytes, want %d", len(got), len(body))
	}
	if last.Received != int64(len(body)) || last.Phase != "downloading" {
		t.Errorf("final progress = %+v", last)
	}
	if last.Percent < 99.9 {
		t.Errorf("final percent = %v, want ~100", last.Percent)
	}
}

// These URLs are short-lived, so a transfer interrupted near the end must not
// have to start over after the launcher fetches a fresh one.
func TestDownloadResumesFromAPartialFile(t *testing.T) {
	body := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	var sawRange string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRange = r.Header.Get("Range")
		http.ServeContent(w, r, "archive.tar.gz", zeroTime, newReadSeeker(body))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "out.part")
	if err := os.WriteFile(path, body[:10], 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Fetch(context.Background(), server.URL, path, int64(len(body)), nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	if sawRange != "bytes=10-" {
		t.Errorf("Range header = %q, want bytes=10-", sawRange)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Errorf("resumed file = %q, want %q", got, body)
	}
}

// A partial file at or beyond the expected size is not resumable — appending to
// it would produce a file that can never verify.
func TestAnOversizedPartialFileIsDiscardedRatherThanResumed(t *testing.T) {
	body := []byte("short")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			t.Errorf("should not have asked to resume, sent %q", r.Header.Get("Range"))
		}
		w.Write(body)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "out.part")
	if err := os.WriteFile(path, []byte("this is much longer than the real body"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Fetch(context.Background(), server.URL, path, int64(len(body)), nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(body) {
		t.Errorf("file = %q, want %q", got, body)
	}
}

func TestDownloadReportsAServerRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	err := Fetch(context.Background(), server.URL, filepath.Join(t.TempDir(), "out.part"), 0, nil)
	if err == nil {
		t.Fatal("want an error for a 403")
	}
}
