//go:build !darwin

package deps

import (
	"context"

	"github.com/gustaavik/wc-launcher/internal/install"
	"github.com/gustaavik/wc-launcher/internal/paths"
)

// Ensure does nothing off macOS.
//
// Windows and Linux get their Vulkan loader from the GPU driver, so there is
// nothing for the launcher to install and a download here would only be a way
// to fail. A player whose driver is genuinely missing is told so by the game,
// which is the only party that knows.
func Ensure(context.Context, paths.Layout, install.ProgressFunc) (string, error) {
	return "", nil
}
