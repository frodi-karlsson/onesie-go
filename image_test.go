package onesie_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/frodi-karlsson/onesie-go"
)

func TestNewImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		data       func(t *testing.T) []byte
		wantFormat string
		wantErr    string
	}{
		{name: "should read a png", data: squarePNG, wantFormat: "png"},
		{name: "should read a png of one plain colour", data: emptyPNG, wantFormat: "png"},
		{name: "should read a jpeg as jpg", data: squareJPEG, wantFormat: "jpg"},
		{
			name:    "should refuse bytes that are neither png nor jpeg",
			data:    func(*testing.T) []byte { return []byte("GIF89a not an image") },
			wantErr: "an image must be PNG or JPEG",
		},
		{
			name:    "should refuse a png whose header is cut short",
			data:    func(t *testing.T) []byte { return squarePNG(t)[:12] },
			wantErr: "the PNG header cannot be read",
		},
		{
			name:    "should refuse empty bytes",
			data:    func(*testing.T) []byte { return nil },
			wantErr: "an image must be PNG or JPEG",
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

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if img.Format != tc.wantFormat {
				t.Errorf("format = %q, want %q", img.Format, tc.wantFormat)
			}
		})
	}
}

func TestImageMarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		image onesie.Image
		want  string
	}{
		{
			name:  "should write the type, the format and standard padded base64",
			image: onesie.Image{Format: "png", Data: []byte{0xff, 0xfe, 0xfd, 0xfc}},
			want:  `{"type":"image","format":"png","data_base64":"//79/A=="}`,
		},
		{
			name:  "should write jpeg as jpg and lowercase the format",
			image: onesie.Image{Format: "JPEG", Data: []byte{1}},
			want:  `{"type":"image","format":"jpg","data_base64":"AQ=="}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(tc.image)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if string(got) != tc.want {
				t.Errorf("json = %s, want %s", got, tc.want)
			}
		})
	}
}
