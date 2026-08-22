package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Progress reports how a download is going.
type Progress struct {
	// Phase is one of "downloading", "verifying", "extracting", "done".
	Phase string `json:"phase"`
	// Received and Total are bytes. Total is 0 when the size is unknown.
	Received int64 `json:"received"`
	Total    int64 `json:"total"`
	// Percent is 0-100, or -1 when Total is unknown.
	Percent float64 `json:"percent"`
}

// ProgressFunc is called as a download advances. It must not block: it runs on
// the copy loop.
type ProgressFunc func(Progress)

// progressInterval throttles progress reports. Every 32 KiB chunk would emit
// thousands of events for a 7 MB file and tens of thousands for a larger one.
const progressInterval = 100 * time.Millisecond

// downloadTimeout bounds a whole transfer. Generous: a slow connection pulling
// a few hundred megabytes is not a failure.
const downloadTimeout = 30 * time.Minute

// download fetches url into path, resuming if a partial file is already there.
//
// Resume matters more than it looks: these URLs are short-lived, so a download
// interrupted near the end would otherwise restart from zero after the launcher
// asks for a fresh one.
func download(ctx context.Context, url, path string, expectSize int64, report ProgressFunc) error {
	resumeFrom := int64(0)
	if info, err := os.Stat(path); err == nil {
		resumeFrom = info.Size()
		// A part file at or past the expected size is finished or wrong; either
		// way, start again rather than guess.
		if expectSize > 0 && resumeFrom >= expectSize {
			resumeFrom = 0
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("discard stale partial download: %w", err)
			}
		}
	}

	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}
	if resumeFrom > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeFrom))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	appending := false
	switch resp.StatusCode {
	case http.StatusOK:
		// The server ignored the range, or we asked for the whole file.
		appending = false
	case http.StatusPartialContent:
		appending = true
	case http.StatusRequestedRangeNotSatisfiable:
		// The partial file is already as long as the resource. Start over.
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("discard stale partial download: %w", err)
		}
		return download(ctx, url, path, expectSize, report)
	default:
		return fmt.Errorf("download failed: the server answered %s", resp.Status)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if appending {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
		resumeFrom = 0
	}
	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	total := expectSize
	if total <= 0 && resp.ContentLength > 0 {
		total = resp.ContentLength + resumeFrom
	}

	if err := copyWithProgress(file, resp.Body, resumeFrom, total, report); err != nil {
		return err
	}
	return file.Sync()
}

func copyWithProgress(dst io.Writer, src io.Reader, already, total int64, report ProgressFunc) error {
	buf := make([]byte, 128*1024)
	received := already
	last := time.Now()

	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			if _, err := dst.Write(buf[:n]); err != nil {
				return fmt.Errorf("write download: %w", err)
			}
			received += int64(n)
			if report != nil && time.Since(last) >= progressInterval {
				report(progressOf("downloading", received, total))
				last = time.Now()
			}
		}
		if readErr == io.EOF {
			if report != nil {
				report(progressOf("downloading", received, total))
			}
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("download interrupted: %w", readErr)
		}
	}
}

func progressOf(phase string, received, total int64) Progress {
	percent := -1.0
	if total > 0 {
		percent = float64(received) / float64(total) * 100
		if percent > 100 {
			percent = 100
		}
	}
	return Progress{Phase: phase, Received: received, Total: total, Percent: percent}
}

// verify checks a file's SHA-256 against wantHex.
//
// An empty wantHex is an error, not a pass. Skipping verification because a
// digest was missing is how an unverified binary gets executed.
func verify(path, wantHex string, report ProgressFunc) error {
	if wantHex == "" {
		return fmt.Errorf("no checksum published for this build; refusing to install it unverified")
	}
	if report != nil {
		report(Progress{Phase: "verifying", Percent: -1})
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	var digest hash.Hash = sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	got := hex.EncodeToString(digest.Sum(nil))
	if !strings.EqualFold(got, wantHex) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", wantHex, got)
	}
	return nil
}

// parseChecksumFile reads a `shasum -a 256` line: "<hex>  <filename>".
func parseChecksumFile(contents string) string {
	fields := strings.Fields(strings.TrimSpace(contents))
	if len(fields) == 0 {
		return ""
	}
	candidate := strings.ToLower(fields[0])
	if len(candidate) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(candidate); err != nil {
		return ""
	}
	return candidate
}
