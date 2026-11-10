package onesie_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/frodi-karlsson/onesie-go"
)

func TestValidateRequest(t *testing.T) {
	t.Parallel()

	square := func(t *testing.T) onesie.Image {
		t.Helper()

		img, err := onesie.NewImage(squarePNG(t))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		return img
	}

	noul := onesie.Questions{{ID: "q", Question: onesie.Noul{Instructions: "a dog in `photo`"}}}
	box := onesie.Questions{{ID: "mug", Question: onesie.Box{Instructions: "the mug in `photo`"}}}

	tests := []struct {
		name    string
		req     func(t *testing.T) onesie.Request
		wantErr string
	}{
		{
			name: "should accept a request without assets",
			req: func(*testing.T) onesie.Request {
				return onesie.Request{State: "s", Questions: noul}
			},
		},
		{
			name: "should accept an image referenced from the state",
			req: func(t *testing.T) onesie.Request {
				return onesie.Request{
					State:     map[string]any{"photo": "image-a"},
					Questions: noul,
					Assets:    onesie.Assets{"image-a": square(t)},
				}
			},
		},
		{
			name: "should accept an image referenced mid sentence in the instructions",
			req: func(t *testing.T) onesie.Request {
				return onesie.Request{
					State: "s",
					Questions: onesie.Questions{
						{ID: "q", Question: onesie.Noul{Instructions: "is this image-a a dog"}},
					},
					Assets: onesie.Assets{"image-a": square(t)},
				}
			},
		},
		{
			name: "should refuse a question error before any asset check",
			req: func(*testing.T) onesie.Request {
				return onesie.Request{State: "s"}
			},
			wantErr: "at least one question is required",
		},
		{
			name: "should refuse an unreferenced asset",
			req: func(t *testing.T) onesie.Request {
				return onesie.Request{State: "s", Questions: noul, Assets: onesie.Assets{"image-a": square(t)}}
			},
			wantErr: "asset 'image-a' is not referenced",
		},
		{
			name: "should not count a reference that is only an object key",
			req: func(t *testing.T) onesie.Request {
				return onesie.Request{
					State:     map[string]any{"image-a": "a photo"},
					Questions: noul,
					Assets:    onesie.Assets{"image-a": square(t)},
				}
			},
			wantErr: "asset 'image-a' is not referenced",
		},
		{
			name: "should refuse a blank reference",
			req: func(t *testing.T) onesie.Request {
				return onesie.Request{
					State: "  ", Questions: noul, Assets: onesie.Assets{"  ": square(t)},
				}
			},
			wantErr: "an asset reference must not be blank",
		},
		{
			name: "should refuse a reference over 1024 bytes",
			req: func(t *testing.T) onesie.Request {
				long := strings.Repeat("r", 1025)

				return onesie.Request{State: long, Questions: noul, Assets: onesie.Assets{long: square(t)}}
			},
			wantErr: "is 1025 bytes, and a reference is at most 1024",
		},
		{
			name: "should refuse a box without an image",
			req: func(*testing.T) onesie.Request {
				return onesie.Request{State: "s", Questions: box}
			},
			wantErr: "box question 'mug' needs an image in the request",
		},
		{
			name: "should refuse a pointer to a box without an image",
			req: func(*testing.T) onesie.Request {
				return onesie.Request{
					State:     "s",
					Questions: onesie.Questions{{ID: "mug", Question: &onesie.Box{Instructions: "the mug"}}},
				}
			},
			wantErr: "box question 'mug' needs an image in the request",
		},
		{
			name: "should refuse an image narrower than 280 pixels",
			req: func(t *testing.T) onesie.Request {
				img, err := onesie.NewImage(sizedPNG(t, 200, 400))
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return onesie.Request{
					State: map[string]any{"photo": "image-a"}, Questions: noul,
					Assets: onesie.Assets{"image-a": img},
				}
			},
			wantErr: "image 'image-a' is 200 by 400 pixels, and each side must be 280 to 1568",
		},
		{
			name: "should refuse an image over 16 MiB",
			req: func(t *testing.T) onesie.Request {
				img := square(t)
				img.Data = append(img.Data, make([]byte, onesie.MaxImageBytes)...)

				return onesie.Request{
					State: map[string]any{"photo": "image-a"}, Questions: noul,
					Assets: onesie.Assets{"image-a": img},
				}
			},
			wantErr: "and an image is at most 16777216",
		},
		{
			name: "should refuse an image whose format is not png or jpg",
			req: func(t *testing.T) onesie.Request {
				img := square(t)
				img.Format = "gif"

				return onesie.Request{
					State: map[string]any{"photo": "image-a"}, Questions: noul,
					Assets: onesie.Assets{"image-a": img},
				}
			},
			wantErr: "image 'image-a' has format 'gif'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := onesie.ValidateRequest(tc.req(t))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}

			if !errors.Is(err, onesie.ErrValidation) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want a validation error containing %q", err, tc.wantErr)
			}
		})
	}

	t.Run("should measure a body with images against the limit, leaving long context out", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			size        int
			longContext bool
			refused     bool
		}{
			{size: onesie.MaxBodyBytes, longContext: true},
			{size: onesie.MaxBodyBytes + 1, refused: true},
		} {
			err := onesie.ValidateRequest(requestOfSize(t, tc.size, "", tc.longContext))
			if refused := errors.Is(err, onesie.ErrValidation); refused != tc.refused {
				t.Errorf("ValidateRequest at %d bytes with long context %v = %v, want refused %v", tc.size, tc.longContext, err, tc.refused)
			}
		}
	})
}

func requestOfSize(t testing.TB, size int, model string, longContext bool) onesie.Request {
	t.Helper()

	state := map[string]any{"photo": "image-a", "other": "image-b", "pad": ""}
	req := onesie.Request{
		Model: model, State: state, Questions: oneNoul(),
		Assets: onesie.Assets{"image-a": {Format: "png"}, "image-b": {Format: "png"}},
	}

	empty, err := onesie.MarshalBody(req)
	if err != nil {
		t.Fatal(err)
	}

	// An image is read only for its header, so a real PNG followed by zero bytes passes validation, and
	// two of them reach the size limit faster than a long string, which the encoder checks byte by byte.
	// Each grows by a multiple of 3 bytes, 4 in base64, and the state takes the 0 to 7 bytes left.
	raw := (size - len(empty)) / 8 * 3
	data := make([]byte, raw)
	copy(data, squarePNG(t))

	req.Assets = onesie.Assets{"image-a": {Format: "png", Data: data}, "image-b": {Format: "png", Data: data}}
	state["pad"] = strings.Repeat("a", size-len(empty)-raw/3*8)
	req.LongContext = longContext

	return req
}

func TestUnreferencedAssets(t *testing.T) {
	t.Parallel()

	img := onesie.Image{Format: "png", Data: []byte{1}}
	questions := onesie.Questions{{ID: "q", Question: onesie.Noul{Instructions: "compare with image-q"}}}

	tests := []struct {
		name string
		req  onesie.Request
		want []string
	}{
		{
			name: "should list an asset only the missing state referenced",
			req: onesie.Request{
				Questions: questions,
				Assets:    onesie.Assets{"image-q": img, "image-s": img},
			},
			want: []string{"image-s"},
		},
		{
			name: "should list nothing when every asset is referenced",
			req: onesie.Request{
				State:     []any{"image-s"},
				Questions: questions,
				Assets:    onesie.Assets{"image-q": img, "image-s": img},
			},
		},
		{
			name: "should take nested names each named on its own",
			req: onesie.Request{
				State:     "photo1 and photo10",
				Questions: questions,
				Assets:    onesie.Assets{"image-q": img, "photo1": img, "photo10": img},
			},
		},
		{
			name: "should list a name found only inside a longer one",
			req: onesie.Request{
				State:     "img2",
				Questions: questions,
				Assets:    onesie.Assets{"image-q": img, "img": img, "img2": img},
			},
			want: []string{"img"},
		},
		{
			name: "should read a raw state's repeated keys as a raw body's are read",
			req: onesie.Request{
				State:     json.RawMessage(`{"photo":"image-s","photo":"none"}`),
				Questions: questions,
				Assets:    onesie.Assets{"image-q": img, "image-s": img},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := onesie.UnreferencedAssets(tc.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !slices.Equal(got, tc.want) {
				t.Errorf("unreferenced = %v, want %v", got, tc.want)
			}
		})
	}
}
