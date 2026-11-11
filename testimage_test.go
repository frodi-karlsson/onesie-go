package onesie_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"testing"
)

// The square sits at x 160 to 320 and y 120 to 240 on a 640 by 480 canvas, so its edges are left
// 0.25, top 0.25, right 0.5 and bottom 0.5.
const (
	canvasWidth  = 640
	canvasHeight = 480
)

func squarePNG(t testing.TB) []byte {
	t.Helper()

	return encodePNG(t, canvas(canvasWidth, canvasHeight, image.Rect(160, 120, 320, 240)))
}

func squareJPEG(t testing.TB) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, canvas(canvasWidth, canvasHeight, image.Rect(160, 120, 320, 240)), nil); err != nil {
		t.Fatalf("encoding the jpeg: %v", err)
	}

	return buf.Bytes()
}

func emptyPNG(t testing.TB) []byte {
	t.Helper()

	return encodePNG(t, canvas(canvasWidth, canvasHeight, image.Rectangle{}))
}

func sizedPNG(t testing.TB, width, height int) []byte {
	t.Helper()

	return encodePNG(t, canvas(width, height, image.Rectangle{}))
}

func encodePNG(t testing.TB, img image.Image) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the png: %v", err)
	}

	return buf.Bytes()
}

func canvas(width, height int, square image.Rectangle) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	red := color.RGBA{R: 255, A: 255}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	for y := range height {
		for x := range width {
			img.Set(x, y, white)

			if image.Pt(x, y).In(square) {
				img.Set(x, y, red)
			}
		}
	}

	return img
}

// noisePNG is a side by side PNG of random pixels, which PNG cannot compress, so it is as large as an
// image of that size gets.
func noisePNG(t testing.TB, side int) []byte {
	t.Helper()

	random := rand.New(rand.NewPCG(1, 2))
	img := image.NewRGBA(image.Rect(0, 0, side, side))

	for y := range side {
		for x := range side {
			img.Set(x, y, color.RGBA{uint8(random.UintN(256)), uint8(random.UintN(256)), uint8(random.UintN(256)), 255})
		}
	}

	return encodePNG(t, img)
}
