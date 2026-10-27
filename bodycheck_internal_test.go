package onesie

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckBodyImages(t *testing.T) {
	t.Parallel()

	asset := `{"type":"image","format":"png","data_base64":"AQ=="}`

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "should accept an asset the state names", body: `{"state":{"p":"img"},"questions":{},"assets":{"img":` + asset + `}}`},
		{
			name: "should accept an asset named mid sentence in the instructions",
			body: `{"state":"s","questions":{"q":{"type":"noul","instructions":"is img a dog"}},"assets":{"img":` + asset + `}}`,
		},
		{
			name:    "should refuse a name found only inside a longer one",
			body:    `{"state":"img2","questions":{},"assets":{"img":` + asset + `,"img2":` + asset + `}}`,
			wantErr: "asset 'img' is not referenced in the state or the questions, and the API refuses that",
		},
		{
			name: "should accept nested names each named on its own",
			body: `{"state":"photo1 and photo10","questions":{},"assets":{"photo1":` + asset + `,"photo10":` + asset + `}}`,
		},
		{
			name:    "should refuse a format that is not a string with the image wording",
			body:    `{"state":"img","questions":{},"assets":{"img":{"format":1,"data_base64":"AQ=="}}}`,
			wantErr: "image 'img' has format '', and an image must be png or jpg",
		},
		{
			name:    "should refuse an asset named only in a name",
			body:    `{"state":{"img":"x"},"questions":{},"assets":{"img":` + asset + `}}`,
			wantErr: "asset 'img' is not referenced in the state or the questions, and the API refuses that",
		},
		{
			name:    "should not count a question's type as a reference",
			body:    `{"state":"s","questions":{"q":{"type":"img"}},"assets":{"img":` + asset + `}}`,
			wantErr: "asset 'img' is not referenced",
		},
		{
			name:    "should refuse a format that is not png or jpg",
			body:    `{"state":"img","questions":{},"assets":{"img":{"format":"gif","data_base64":"AQ=="}}}`,
			wantErr: "image 'img' has format 'gif', and an image must be png or jpg",
		},
		{
			name:    "should refuse data that is not a string",
			body:    `{"state":"img","questions":{},"assets":{"img":{"format":"jpeg","data_base64":1}}}`,
			wantErr: "asset 'img' needs data_base64 as a string",
		},
		{
			name:    "should refuse assets that are not an object",
			body:    `{"state":"img","questions":{},"assets":["img"]}`,
			wantErr: "assets must be an object of images",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := checkBodyImages([]byte(tc.body))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}

			if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
