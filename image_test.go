package onesie_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/frodi-karlsson/onesie-go"
)

func TestNewImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		data       func(t testing.TB) []byte
		wantFormat string
		wantErr    string
		wantCause  error
	}{
		{name: "should read a png", data: squarePNG, wantFormat: "png"},
		{name: "should read a png of one plain colour", data: emptyPNG, wantFormat: "png"},
		{name: "should read a jpeg as jpg", data: squareJPEG, wantFormat: "jpg"},
		{
			name:    "should refuse bytes that are neither png nor jpeg",
			data:    func(testing.TB) []byte { return []byte("GIF89a not an image") },
			wantErr: "an image must be PNG or JPEG",
		},
		{
			name:      "should refuse a png whose header is cut short",
			data:      func(t testing.TB) []byte { return squarePNG(t)[:12] },
			wantErr:   "the PNG header cannot be read",
			wantCause: io.ErrUnexpectedEOF,
		},
		{
			name:    "should refuse empty bytes",
			data:    func(testing.TB) []byte { return nil },
			wantErr: "an image must be PNG or JPEG",
		},
		{
			name:    "should refuse an image narrower than 280 pixels",
			data:    func(t testing.TB) []byte { return sizedPNG(t, 200, 400) },
			wantErr: "the image is 200 by 400 pixels, and each side must be 280 to 1568",
		},
		{
			name:    "should refuse an image shorter than 280 pixels",
			data:    func(t testing.TB) []byte { return sizedPNG(t, 400, 200) },
			wantErr: "the image is 400 by 200 pixels",
		},
		{
			name:    "should refuse an image wider than 1568 pixels",
			data:    func(t testing.TB) []byte { return sizedPNG(t, 1600, 400) },
			wantErr: "the image is 1600 by 400 pixels",
		},
		{
			name:    "should refuse an image over 16 MiB",
			data:    func(t testing.TB) []byte { return append(squarePNG(t), make([]byte, onesie.MaxImageBytes)...) },
			wantErr: "and an image is at most 16777216",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			img, err := onesie.NewImage(tc.data(t))
			if tc.wantErr != "" {
				if !errors.Is(err, onesie.ErrValidation) || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want a validation error containing %q", err, tc.wantErr)
				}

				if tc.wantCause != nil && !errors.Is(err, tc.wantCause) {
					t.Errorf("error = %v, want it to carry %v", err, tc.wantCause)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if img.Format() != tc.wantFormat {
				t.Errorf("format = %q, want %q", img.Format(), tc.wantFormat)
			}
		})
	}

	t.Run("should keep the file it checked when the caller changes its slice", func(t *testing.T) {
		t.Parallel()

		data := squarePNG(t)
		want := `{"type":"image","format":"png","data_base64":"` + base64.StdEncoding.EncodeToString(data) + `"}`

		img, err := onesie.NewImage(data)
		if err != nil {
			t.Fatal(err)
		}

		clear(data)

		if got, err := json.Marshal(img); err != nil || string(got) != want {
			t.Errorf("json = %s, %v, want the file as checked", got, err)
		}
	})
}

func TestImageMarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		data   func(t testing.TB) []byte
		format string
	}{
		{name: "should write the type, the format and standard padded base64", data: squarePNG, format: "png"},
		{name: "should write a jpeg as jpg", data: squareJPEG, format: "jpg"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data := tc.data(t)

			img, err := onesie.NewImage(data)
			if err != nil {
				t.Fatal(err)
			}

			got, err := json.Marshal(img)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			want := `{"type":"image","format":"` + tc.format + `","data_base64":"` + base64.StdEncoding.EncodeToString(data) + `"}`
			if string(got) != want {
				t.Errorf("json = %s, want %s", got, want)
			}
		})
	}

	t.Run("should hand out a copy, so a caller changing it changes nothing", func(t *testing.T) {
		t.Parallel()

		img, err := onesie.NewImage(squarePNG(t))
		if err != nil {
			t.Fatal(err)
		}

		first, err := img.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}

		want := string(first)
		for i := range first {
			first[i] = 'x'
		}

		if again, err := img.MarshalJSON(); err != nil || string(again) != want {
			t.Errorf("MarshalJSON after a caller changed its result = %.60s, %v, want %.60s", again, err, want)
		}
	})
}

func imageOf(t testing.TB, data []byte) onesie.Image {
	t.Helper()

	img, err := onesie.NewImage(data)
	if err != nil {
		t.Fatal(err)
	}

	return img
}
