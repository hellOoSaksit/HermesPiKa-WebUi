// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build ignore

// gen draws the HPK Console app icons (the pink "P" mark from index.html) as
// SVG and PNG, with no image library beyond the standard one. Run it after a
// change to the mark:
//
//	go run tools/icons/gen.go
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

const out = "assets/console/icons"

var (
	tile = color.NRGBA{0xEC, 0x79, 0xC0, 0xFF} // --accent (dark theme)
	ink  = color.NRGBA{0x25, 0x08, 0x1B, 0xFF} // --on-accent
)

// The mark lives in a 28×28 box (same numbers as the inline SVG).
const markSVG = `<path d="M9 9v10M9 14h5a4 4 0 0 0 4-4" stroke="#25081B" stroke-width="1.8" fill="none" stroke-linecap="round"/><circle cx="9" cy="8" r="2.4" fill="#25081B"/><circle cx="9" cy="20" r="2.4" fill="#25081B"/><circle cx="19" cy="9" r="2.4" fill="#25081B"/>`

func main() {
	write("icon.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 28 28"><rect width="28" height="28" rx="8" fill="#EC79C0"/>`+markSVG+`</svg>`+"\n"))
	// Maskable: full-bleed tile, mark kept inside the 80% safe circle.
	write("icon-maskable.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 28 28"><rect width="28" height="28" fill="#EC79C0"/><g transform="translate(14 14) scale(0.62) translate(-14 -14)">`+markSVG+`</g></svg>`+"\n"))
	for _, n := range []int{192, 512} {
		writePNG(fmt.Sprintf("icon-%d.png", n), draw(n, false))
	}
	writePNG("icon-maskable-512.png", draw(512, true))
	writePNG("apple-touch-icon.png", draw(180, true)) // iOS rounds the corners itself
}

func draw(n int, maskable bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	const ss = 4 // samples per axis
	scale := 28.0 / float64(n)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := (float64(x) + (float64(sx)+0.5)/ss) * scale
					py := (float64(y) + (float64(sy)+0.5)/ss) * scale
					c, ok := sample(px, py, maskable)
					if !ok {
						continue
					}
					r += float64(c.R)
					g += float64(c.G)
					b += float64(c.B)
					a++
				}
			}
			if a == 0 {
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(255 * a / ss / ss)})
		}
	}
	return img
}

// sample says what colour (if any) covers the point (x, y) in the 28-unit box.
func sample(x, y float64, maskable bool) (color.NRGBA, bool) {
	if !maskable && !inRoundRect(x, y, 28, 8) {
		return color.NRGBA{}, false
	}
	mx, my := x, y
	if maskable { // undo translate(14 14) scale(0.62) translate(-14 -14)
		mx = (x-14)/0.62 + 14
		my = (y-14)/0.62 + 14
	}
	if inMark(mx, my) {
		return ink, true
	}
	return tile, true
}

func inRoundRect(x, y, size, r float64) bool {
	cx := math.Max(r, math.Min(size-r, x))
	cy := math.Max(r, math.Min(size-r, y))
	return math.Hypot(x-cx, y-cy) <= r
}

func inMark(x, y float64) bool {
	const hw = 0.9 // half the stroke width
	for _, c := range [][2]float64{{9, 8}, {9, 20}, {19, 9}} {
		if math.Hypot(x-c[0], y-c[1]) <= 2.4 {
			return true
		}
	}
	if segDist(x, y, 9, 9, 9, 19) <= hw || segDist(x, y, 9, 14, 14, 14) <= hw {
		return true
	}
	// a 4 4 0 0 0 4 -4 from (14,14) to (18,10): quarter circle around (14,10).
	if x >= 14 && y >= 10 && math.Abs(math.Hypot(x-14, y-10)-4) <= hw {
		return true
	}
	return math.Hypot(x-18, y-10) <= hw // round cap at the arc's end
}

func segDist(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := ((px-ax)*dx + (py-ay)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}

func write(name string, b []byte) {
	if err := os.WriteFile(filepath.Join(out, name), b, 0o644); err != nil {
		panic(err)
	}
}

func writePNG(name string, img image.Image) {
	f, err := os.Create(filepath.Join(out, name))
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(f, img); err != nil {
		panic(err)
	}
}
