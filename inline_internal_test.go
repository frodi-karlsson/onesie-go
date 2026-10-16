package onesie

import (
	"errors"
	"strings"
	"testing"
)

func TestInlineParts(t *testing.T) {
	t.Parallel()

	part := func(mime, data string) string {
		return `{"type":"image_url","image_url":{"url":"data:` + mime + `;base64,` + data + `"}}`
	}

	a := part("image/png", "AQ==")
	b := part("image/jpeg", "Ag==")
	assets := `"assets":{"img-a":{"type":"image","format":"png","data_base64":"AQ=="},` +
		`"img-ab":{"type":"image","format":"JPEG","data_base64":"Ag=="}}`

	tests := []struct {
		name    string
		body    string
		want    string
		wantErr string
	}{
		{
			name: "should put a part where a state value is a reference and drop assets",
			body: `{"state":{"p":"img-a","q":"img-ab"},"model":"m","questions":{},` + assets + `}`,
			want: `{"state":{"p":` + a + `,"q":` + b + `},"model":"m","questions":{}}`,
		},
		{
			name: "should read assets that come before the state",
			body: `{` + assets + `,"state":["img-ab","img-a"],"questions":{}}`,
			want: `{"state":[` + b + `,` + a + `],"questions":{}}`,
		},
		{
			name: "should make a bare string state that is a reference into its part",
			body: `{"state":"img-a","questions":{"q":{"type":"noul","instructions":"img-ab"}},` + assets + `}`,
			want: `{"state":` + a + `,"questions":{"q":{"type":"noul","instructions":` + b + `}}}`,
		},
		{
			name: "should split a sentence around the references in it, the longer reference winning",
			body: `{"state":"see img-a then img-ab.","questions":{},` + assets + `}`,
			want: `{"state":["see ",` + a + `," then ",` + b + `,"."],"questions":{}}`,
		},
		{
			name: "should replace references in instructions and criteria",
			body: `{"state":"s","questions":{"x":{"type":"noul","instructions":"is img-ab?",` +
				`"criteria":{"true":["img-a"],"false":"no"}}},` + assets + `}`,
			want: `{"state":"s","questions":{"x":{"type":"noul","instructions":["is ",` + b + `,"?"],` +
				`"criteria":{"true":[` + a + `],"false":"no"}}}}`,
		},
		{
			name: "should leave names, a question id, the model and a question type alone",
			body: `{"model":"img-a","state":{"img-a":"x","p":"img-a"},"questions":{"img-ab":{"type":"img-a",` +
				`"instructions":"q img-ab"}},"extra":"img-a",` + assets + `}`,
			want: `{"model":"img-a","state":{"img-a":"x","p":` + a + `},"questions":{"img-ab":{"type":"img-a",` +
				`"instructions":["q ",` + b + `]}},"extra":"img-a"}`,
		},
		{
			name: "should keep numbers, escapes and repeated names as written",
			body: "{\"state\":{\"n\":12345678901234567890123,\"f\":1.50e3,\"u\":\"x\\u00e9 <&>\"," +
				"\"d\":1,\"d\":2,\"p\":\"img-a\"},\"questions\":{\"q\":{\"type\":\"noul\",\"instructions\":\"img-ab\"}}," +
				assets + "}",
			want: "{\"state\":{\"n\":12345678901234567890123,\"f\":1.50e3,\"u\":\"x\\u00e9 <&>\"," +
				"\"d\":1,\"d\":2,\"p\":" + a + "},\"questions\":{\"q\":{\"type\":\"noul\",\"instructions\":" + b + "}}}",
		},
		{
			name:    "should refuse an asset nothing references",
			body:    `{"state":"img-a","questions":{},` + assets + `}`,
			wantErr: "asset 'img-ab' is not referenced",
		},
		{
			name:    "should refuse an asset whose format is not png or jpg",
			body:    `{"state":"img-a","questions":{},"assets":{"img-a":{"format":"gif","data_base64":"AQ=="}}}`,
			wantErr: "asset 'img-a' needs a format, png or jpg",
		},
		{
			name:    "should refuse an asset whose data is not a string",
			body:    `{"state":"img-a","questions":{},"assets":{"img-a":{"format":"png","data_base64":1}}}`,
			wantErr: "asset 'img-a' needs data_base64 as a string",
		},
		{
			name:    "should refuse assets that are not an object",
			body:    `{"state":"img-a","questions":{},"assets":[1]}`,
			wantErr: "assets must be an object of images",
		},
		{
			name: "should refuse a body the repeated images push over the limit",
			body: `{"state":"` + strings.Repeat("img-a ", 1000) + `","questions":{},"assets":{"img-a":` +
				`{"format":"png","data_base64":"` + strings.Repeat("A", MaxBodyBytes/900) + `"}}}`,
			wantErr: "name each image once",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, opts, err := inlineParts([]byte(tc.body))
			if tc.wantErr != "" {
				if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want a validation error containing %q", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(opts) != 0 {
				t.Errorf("options = %d, want none", len(opts))
			}

			if string(got) != tc.want {
				t.Errorf("body\ngot  %s\nwant %s", got, tc.want)
			}
		})
	}
}
