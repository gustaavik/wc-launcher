package selfupdate

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeBundle builds the smallest thing codesign will treat as an app bundle: a
// real Mach-O (borrowed from the system) with an Info.plist naming it.
func fakeBundle(t *testing.T) string {
	t.Helper()
	app := filepath.Join(t.TempDir(), "Fake.app")
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	binary, err := os.ReadFile("/bin/echo")
	if err != nil {
		t.Skipf("no system binary to borrow: %v", err)
	}
	if err := os.WriteFile(filepath.Join(macos, "Fake"), binary, 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>Fake</string>
<key>CFBundleIdentifier</key><string>com.example.fake</string>
</dict></plist>
`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}
	return app
}

// The gate that a compromised GitHub account cannot get past: an attacker who
// can publish a release still cannot sign one with this team's Developer ID.
func TestVerifySignatureRejectsABuildThatIsNotOurs(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("codesign is macOS only")
	}
	app := fakeBundle(t)

	// Ad-hoc: structurally valid, correctly signed, and signed by nobody.
	if out, err := exec.Command("/usr/bin/codesign", "--force", "--deep", "--sign", "-", app).CombinedOutput(); err != nil {
		t.Skipf("could not ad-hoc sign a test bundle: %v: %s", err, out)
	}

	err := verifySignature(app)
	if err == nil {
		t.Fatal("an ad-hoc signed bundle was accepted")
	}
	if !strings.Contains(err.Error(), "unexpected developer") {
		t.Errorf("rejected for the wrong reason: %v", err)
	}
}

func TestVerifySignatureRejectsAnUnsignedBuild(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("codesign is macOS only")
	}

	if err := verifySignature(fakeBundle(t)); err == nil {
		t.Fatal("an unsigned bundle was accepted")
	}
}

// Points at a real Developer ID signed bundle when there is one, which is the
// only way to confirm the accept side rather than just the reject side:
//
//	WC_IT_SIGNED_APP=bin/Wyvencraft.app go test ./internal/selfupdate/
func TestVerifySignatureAcceptsARealBuild(t *testing.T) {
	app := os.Getenv("WC_IT_SIGNED_APP")
	if app == "" {
		t.Skip("set WC_IT_SIGNED_APP to a Developer ID signed .app")
	}
	if err := verifySignature(app); err != nil {
		t.Fatalf("verifySignature(%s): %v", app, err)
	}
}
