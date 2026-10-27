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
			name:    "should refuse an asset whose format is not png or jpg",
			body:    `{"state":"img-a","questions":{},"assets":{"img-a":{"format":"gif","data_base64":"AQ=="}}}`,
			wantErr: "image 'img-a' has format 'gif', and an image must be png or jpg",
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

			got, err := inlineParts(parseBody([]byte(tc.body)).members)
			if tc.wantErr != "" {
				if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want a validation error containing %q", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			out, writeErr := writeMembers(got)
			if writeErr != nil || string(out) != tc.want {
				t.Errorf("body\ngot  %s\nwant %s", out, tc.want)
			}
		})
	}
}

func TestAssetsToImages(t *testing.T) {
	t.Parallel()

	const (
		r1 = `"r1":{"type":"image","format":"png","data_base64":"AQ=="}`
		r2 = `"r2":{"type":"image","format":"JPG","data_base64":"Ag=="}`
	)

	tests := []struct {
		name    string
		body    string
		want    string
		wantErr string
	}{
		{
			name: "should number the images by first reference and write them last",
			body: `{"state":{"a":"r2","b":"r1"},"questions":{"q":{"type":"noul","instructions":"is r1 a dog"}},` +
				`"assets":{` + r1 + `,` + r2 + `}}`,
			want: `{"state":{"a":"image 1","b":"image 2"},"questions":{"q":{"type":"noul","instructions":"is image 2 a dog"}},` +
				`"images":["data:image/jpeg;base64,Ag==","data:image/png;base64,AQ=="]}`,
		},
		{
			name: "should number by the body as written when the questions come first",
			body: `{"questions":{"q":{"type":"noul","instructions":"compare r1 and r2"}},"state":"see r2",` +
				`"assets":{` + r1 + `,` + r2 + `}}`,
			want: `{"questions":{"q":{"type":"noul","instructions":"compare image 1 and image 2"}},"state":"see image 2",` +
				`"images":["data:image/png;base64,AQ==","data:image/jpeg;base64,Ag=="]}`,
		},
		{
			name: "should keep one position for a name used twice and leave names and types alone",
			body: `{"state":{"r1":"r1 then r1"},"questions":{"r1":{"type":"noul","instructions":"q"}},"assets":{` + r1 + `}}`,
			want: `{"state":{"r1":"image 1 then image 1"},"questions":{"r1":{"type":"noul","instructions":"q"}},` +
				`"images":["data:image/png;base64,AQ=="]}`,
		},
		{
			name: "should leave a question's type alone when an asset shares its text",
			body: `{"state":"noul","questions":{"q":{"type":"noul","instructions":"q"}},` +
				`"assets":{"noul":{"format":"png","data_base64":"AQ=="}}}`,
			want: `{"state":"image 1","questions":{"q":{"type":"noul","instructions":"q"}},` +
				`"images":["data:image/png;base64,AQ=="]}`,
		},
		{
			name:    "should refuse a body with both assets and images",
			body:    `{"state":"r1","questions":{},"images":["data:image/png;base64,AQ=="],"assets":{` + r1 + `}}`,
			wantErr: "onesie: a body for clef carries assets or images, not both",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := assetsToImages(parseBody([]byte(tc.body)).members)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			out, writeErr := writeMembers(got)
			if writeErr != nil || string(out) != tc.want {
				t.Errorf("body\ngot  %s\nwant %s", out, tc.want)
			}
		})
	}
}
