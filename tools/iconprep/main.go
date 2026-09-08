// Command iconprep derives the icon assets that cannot be consumed straight from
// build/appicon.png.
//
// The artwork is exported with a near-transparent white film covering the whole
// canvas — two thirds of its pixels carry an alpha of about 4/255. That film is
// invisible on its own, but it makes the image full-bleed, so Icon Composer has
// no content box to position against and drapes the group's shadow and specular
// highlight over the full square instead of the letter. Dropping every pixel
// below alphaFloor removes it without touching real antialiasing, which lives
// far above the cut.
//
// Two assets come out of that cleaned image:
//
//	build/appicon.icon/Assets/appicon.png   the Icon Composer layer, trimmed to
//	                                        the letter and centred in a square so
//	                                        icon.json's "scale" means exactly how
//	                                        much of the plate the letter covers
//	frontend/public/logo.png                the in-app logo, trimmed and scaled to
//	                                        logoMax on its longest side
//
// Run it from the repo root (`go run ./tools/iconprep`); build/Taskfile.yml wires
// it in ahead of `wails3 generate icons`.
package main

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

const (
	// Alpha at or above which a pixel counts as artwork rather than film. The
	// source histogram is bimodal — 66.7% of pixels sit at alpha 0-15 and 32.3%
	// at 240-255, with barely 1% in between — so anything in single digits is
	// film and anything the eye can see clears this by a wide margin.
	alphaFloor = 8

	// Longest side of the in-app logo. The largest on-screen use is the 44px
	// loading splash, so this covers it at DPR 3 with room spare.
	logoMax = 256

	srcPath   = "build/appicon.png"
	layerPath = "build/appicon.icon/Assets/appicon.png"
	logoPath  = "frontend/public/logo.png"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "iconprep:", err)
		os.Exit(1)
	}
}

func run() error {
	src, err := load(srcPath)
	if err != nil {
		return err
	}

	cleaned := stripFilm(src)
	box := contentBounds(cleaned)
	if box.Empty() {
		return fmt.Errorf("%s has no pixels at or above alpha %d", srcPath, alphaFloor)
	}

	trimmed := crop(cleaned, box)
	fmt.Printf("trimmed %s to %dx%d at %v\n", srcPath, trimmed.Rect.Dx(), trimmed.Rect.Dy(), box)

	if err := write(layerPath, square(trimmed)); err != nil {
		return err
	}
	if err := write(logoPath, fit(trimmed, logoMax)); err != nil {
		return err
	}
	return nil
}

// stripFilm returns a copy of img with every pixel below alphaFloor made fully
// transparent. Everything else is carried over untouched.
func stripFilm(img image.Image) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)

	for i := 0; i < len(out.Pix); i += 4 {
		if out.Pix[i+3] < alphaFloor {
			out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = 0, 0, 0, 0
		}
	}
	return out
}

// contentBounds is the tightest rectangle holding every non-transparent pixel.
func contentBounds(img *image.RGBA) image.Rectangle {
	b := img.Bounds()
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X-1, b.Min.Y-1

	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.Pix[img.PixOffset(x, y)+3] == 0 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if maxX < minX {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX+1, maxY+1)
}

func crop(img *image.RGBA, r image.Rectangle) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Bounds(), img, r.Min, draw.Src)
	return out
}

// square centres img on a transparent canvas whose side is its longest edge, so
// the letter's own proportions survive and icon.json positions against a known
// box rather than whatever margin the export happened to leave.
func square(img *image.RGBA) *image.RGBA {
	side := img.Rect.Dx()
	if h := img.Rect.Dy(); h > side {
		side = h
	}
	out := image.NewRGBA(image.Rect(0, 0, side, side))
	at := image.Pt((side-img.Rect.Dx())/2, (side-img.Rect.Dy())/2)
	draw.Draw(out, image.Rectangle{Min: at, Max: at.Add(img.Rect.Size())}, img, image.Point{}, draw.Src)
	return out
}

// fit scales img down so its longest side is max, leaving it alone if it is
// already that small — upscaling would only invent detail.
func fit(img *image.RGBA, max int) *image.RGBA {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	longest := w
	if h > longest {
		longest = h
	}
	if longest <= max {
		return img
	}
	dw := w * max / longest
	dh := h * max / longest
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	return resize(img, dw, dh)
}

// resize area-averages img down to dw x dh. Averaging happens in premultiplied
// alpha — which is what image.RGBA already stores — so transparent pixels
// contribute no colour and edges keep their halo-free antialiasing.
func resize(img *image.RGBA, dw, dh int) *image.RGBA {
	sw, sh := img.Rect.Dx(), img.Rect.Dy()
	out := image.NewRGBA(image.Rect(0, 0, dw, dh))

	for dy := range dh {
		y0, y1 := dy*sh/dh, (dy+1)*sh/dh
		if y1 == y0 {
			y1++
		}
		for dx := range dw {
			x0, x1 := dx*sw/dw, (dx+1)*sw/dw
			if x1 == x0 {
				x1++
			}

			var r, g, b, a, n float64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					i := img.PixOffset(x, y)
					r += float64(img.Pix[i])
					g += float64(img.Pix[i+1])
					b += float64(img.Pix[i+2])
					a += float64(img.Pix[i+3])
					n++
				}
			}

			i := out.PixOffset(dx, dy)
			out.Pix[i] = round(r / n)
			out.Pix[i+1] = round(g / n)
			out.Pix[i+2] = round(b / n)
			out.Pix[i+3] = round(a / n)
		}
	}
	return out
}

func round(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func load(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return img, nil
}

func write(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}

	if err := png.Encode(f, img); err != nil {
		f.Close()
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return err
	}

	fmt.Printf("wrote %s (%dx%d)\n", path, img.Bounds().Dx(), img.Bounds().Dy())
	return nil
}
