package services

import (
	"strings"
	"testing"

	"github.com/gustaavik/wc-launcher/internal/selfupdate"
)

// Windows has no Applications folder; the fix there is a per-user install.
func TestTheUnwritableAdviceSpeaksTheOSsLanguage(t *testing.T) {
	win := unwritableMessageFor("windows", selfupdate.Target{Path: `C:\Program Files\Wyvencraft\Wyvencraft.exe`})
	if strings.Contains(win, "Applications") || !strings.Contains(win, "user account") {
		t.Errorf("windows advice = %q", win)
	}
	mac := unwritableMessageFor("darwin", selfupdate.Target{Path: "/Volumes/dmg/Wyvencraft.app"})
	if !strings.Contains(mac, "Applications folder") {
		t.Errorf("mac advice = %q", mac)
	}
}
