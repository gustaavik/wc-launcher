package install

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// maxEntrySize caps a single unpacked file at 2 GiB.
//
// A gzip stream can claim to be far larger than it is, so an unbounded copy
// turns a small download into a filled disk. The largest real entry is the game
// binary, tens of megabytes.
const maxEntrySize = 2 << 30

// Extract unpacks an archive into dest, which must already exist.
//
// The archive's single top-level directory is stripped, so dest ends up holding
// the binary and assets/ directly rather than one directory containing them.
// That is what the release workflow produces: `wyvencraft-<tag>-<triple>/`.
func Extract(archivePath, dest string) error {
	if strings.HasSuffix(archivePath, ".zip") {
		return extractZip(archivePath, dest)
	}
	return extractTarGz(archivePath, dest)
}

func extractTarGz(archivePath, dest string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", archivePath, err)
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("read %s as gzip: %w", filepath.Base(archivePath), err)
	}
	defer gz.Close()

	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", filepath.Base(archivePath), err)
		}

		target, ok := resolve(dest, header.Name)
		if !ok {
			// A traversing or absolute entry. Refuse the whole archive rather
			// than skipping the entry: a build that contains one is not a build
			// worth installing half of.
			return fmt.Errorf("%s contains an unsafe path %q", filepath.Base(archivePath), header.Name)
		}
		if target == "" {
			continue // the stripped top-level directory
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := writeFile(target, reader, header.FileInfo().Mode()); err != nil {
				return err
			}
		case tar.TypeSymlink, tar.TypeLink:
			// Links are refused outright. A symlink is the other way to escape
			// dest, and the release workflow does not produce any — so this
			// costs nothing and closes the hole.
			return fmt.Errorf("%s contains a link (%q), which is not expected in a game build",
				filepath.Base(archivePath), header.Name)
		default:
			// Devices, fifos, sparse entries: nothing a game build contains.
			continue
		}
	}
}

func extractZip(archivePath, dest string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("read %s as zip: %w", filepath.Base(archivePath), err)
	}
	defer reader.Close()

	for _, entry := range reader.File {
		target, ok := resolve(dest, entry.Name)
		if !ok {
			return fmt.Errorf("%s contains an unsafe path %q", filepath.Base(archivePath), entry.Name)
		}
		if target == "" {
			continue
		}

		info := entry.FileInfo()
		if info.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s contains a symlink (%q), which is not expected in a game build",
				filepath.Base(archivePath), entry.Name)
		}

		source, err := entry.Open()
		if err != nil {
			return fmt.Errorf("read %s from %s: %w", entry.Name, filepath.Base(archivePath), err)
		}
		err = writeFile(target, source, info.Mode())
		source.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// resolve turns an archive entry name into an absolute path under dest, with
// the single top-level directory stripped.
//
// Returns ok=false for anything that would land outside dest, and an empty path
// for the stripped top-level directory itself. Both checks matter: the name
// comes from a downloaded file, and "../../.ssh/authorized_keys" is a valid tar
// entry name.
func resolve(dest, name string) (target string, ok bool) {
	// Archive paths are slash-separated regardless of platform, and a Windows
	// zip may still use backslashes — normalise before judging.
	name = strings.ReplaceAll(name, `\`, "/")
	if path.IsAbs(name) || strings.HasPrefix(name, "/") {
		return "", false
	}
	// A Windows drive letter is absolute too, and path.IsAbs does not say so.
	if len(name) >= 2 && name[1] == ':' {
		return "", false
	}

	cleaned := path.Clean(name)
	if cleaned == "." || cleaned == "/" {
		return "", true
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}

	// Strip the archive's single top-level directory.
	_, rest, hasSlash := strings.Cut(cleaned, "/")
	if !hasSlash {
		// The top-level directory entry itself. Nothing to create: dest is it.
		return "", true
	}
	if rest == "" {
		return "", true
	}

	target = filepath.Join(dest, filepath.FromSlash(rest))

	// Belt and braces. The checks above should make this unreachable, but the
	// consequence of being wrong is writing anywhere on disk, so it is verified
	// rather than reasoned about.
	rel, err := filepath.Rel(dest, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return target, true
}

func writeFile(target string, source io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(target), err)
	}

	// The archive's mode is honoured so the game binary stays executable, but
	// only the bits that make sense for a file we just downloaded.
	perm := mode.Perm() & 0o755
	if perm == 0 {
		perm = 0o644
	}

	file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	defer file.Close()

	written, err := io.Copy(file, io.LimitReader(source, maxEntrySize))
	if err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	if written == maxEntrySize {
		return fmt.Errorf("%s is implausibly large; refusing to unpack it", filepath.Base(target))
	}
	return nil
}
