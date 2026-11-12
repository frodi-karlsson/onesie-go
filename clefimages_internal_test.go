package onesie

import (
	"errors"
	"strings"
	"testing"
)

func TestAssetsToImages(t *testing.T) {
	t.Parallel()

	const (
		r1 = `"r1":{"type":"image","format":"png","data_base64":"AQ=="}`
		r2 = `"r2":{"type":"image","format":"JPG","data_base64":"Ag=="}`

		// img-a is a prefix of img-ab, so a reference to img-ab is matched by its longer name.
		prefixed = `"assets":{"img-a":{"type":"image","format":"png","data_base64":"AQ=="},` +
			`"img-ab":{"type":"image","format":"JPEG","data_base64":"Ag=="}}`
		png  = `"data:image/png;base64,AQ=="`
		jpeg = `"data:image/jpeg;base64,Ag=="`
	)

	tests := []struct {
		name    string
		body    string
		want    string
		wantErr string
	}{
		{
			name: "should escape what JSON needs in base64 text a raw body wraps or garbles",
			body: `{"state":"img-a","questions":{"q":{"type":"noul"}},"assets":{"img-a":{"type":"image","format":"png","data_base64":"ab\ncd<x>&\"q\\ é"}}}`,
			want: `{"state":"image 1","questions":{"q":{"type":"noul"}},"images":["data:image/png;base64,ab\ncd<x>&\"q\\ é"]}`,
		},
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
			name: "should read a format by its exact name and the last of repeated ones",
			body: `{"state":"r1","questions":{"q":{"type":"noul"}},"assets":{"r1":{"format":"jpg","format":"png","Format":"jpg","data_base64":"AQ=="}}}`,
			want: `{"state":"image 1","questions":{"q":{"type":"noul"}},"images":["data:image/png;base64,AQ=="]}`,
		},
		{
			name: "should read the last of repeated assets whole",
			body: `{"state":"r1","questions":{"q":{"type":"noul"}},"assets":{"r1":{"format":"jpg","data_base64":"Ag=="},"r1":{"format":"png","data_base64":"AQ=="}}}`,
			want: `{"state":"image 1","questions":{"q":{"type":"noul"}},"images":["data:image/png;base64,AQ=="]}`,
		},
		{
			name: "should read data by its exact name",
			body: `{"state":"r1","questions":{"q":{"type":"noul"}},"assets":{"r1":{"format":"png","data_base64":"AQ==","Data_Base64":"Ag=="}}}`,
			want: `{"state":"image 1","questions":{"q":{"type":"noul"}},"images":["data:image/png;base64,AQ=="]}`,
		},
		{
			name: "should read assets that come before the state",
			body: `{` + prefixed + `,"state":["img-ab","img-a"],"questions":{"q":{"type":"noul"}}}`,
			want: `{"state":["image 1","image 2"],"questions":{"q":{"type":"noul"}},"images":[` + jpeg + `,` + png + `]}`,
		},
		{
			name: "should split a sentence around the references in it, the longer reference winning",
			body: `{"state":"see img-a then img-ab.","questions":{"q":{"type":"noul"}},` + prefixed + `}`,
			want: `{"state":"see image 1 then image 2.","questions":{"q":{"type":"noul"}},"images":[` + png + `,` + jpeg + `]}`,
		},
		{
			name: "should replace references in instructions and criteria",
			body: `{"state":"s","questions":{"x":{"type":"noul","instructions":"is img-ab?",` +
				`"criteria":{"true":["img-a"],"false":"no"}}},` + prefixed + `}`,
			want: `{"state":"s","questions":{"x":{"type":"noul","instructions":"is image 1?",` +
				`"criteria":{"true":["image 2"],"false":"no"}}},"images":[` + jpeg + `,` + png + `]}`,
		},
		{
			name: "should leave names, a question id, the model and other members alone",
			body: `{"model":"img-a","state":{"img-a":"x","p":"img-a"},"questions":{"img-ab":{"type":"noul",` +
				`"instructions":"q img-ab"}},"extra":"img-a",` + prefixed + `}`,
			want: `{"model":"img-a","state":{"img-a":"x","p":"image 1"},"questions":{"img-ab":{"type":"noul",` +
				`"instructions":"q image 2"}},"extra":"img-a","images":[` + png + `,` + jpeg + `]}`,
		},
		{
			name: "should keep numbers, escapes and repeated names as written",
			body: "{\"state\":{\"n\":12345678901234567890123,\"f\":1.50e3,\"u\":\"x\\u00e9 <&>\"," +
				"\"d\":1,\"d\":2,\"p\":\"img-a\"},\"questions\":{\"q\":{\"type\":\"noul\"}}," + prefixed + "}",
			want: "{\"state\":{\"n\":12345678901234567890123,\"f\":1.50e3,\"u\":\"x\\u00e9 <&>\"," +
				"\"d\":1,\"d\":2,\"p\":\"image 1\"},\"questions\":{\"q\":{\"type\":\"noul\"}},\"images\":[" + png + "]}",
		},
		{
			name:    "should refuse an asset whose format is not png or jpg",
			body:    `{"state":"img-a","questions":{"q":{"type":"noul"}},"assets":{"img-a":{"format":"gif","data_base64":"AQ=="}}}`,
			wantErr: "image 'img-a' has format 'gif', and an image must be png or jpg",
		},
		{
			name:    "should refuse an asset whose data is not a string",
			body:    `{"state":"img-a","questions":{"q":{"type":"noul"}},"assets":{"img-a":{"format":"png","data_base64":1}}}`,
			wantErr: "asset 'img-a' needs data_base64 as a string",
		},
		{
			name:    "should refuse assets that are not an object",
			body:    `{"state":"img-a","questions":{"q":{"type":"noul"}},"assets":[1]}`,
			wantErr: "assets must be an object of images",
		},
		{
			name:    "should refuse a body with both assets and images",
			body:    `{"state":"r1","questions":{"q":{"type":"noul"}},"images":["data:image/png;base64,AQ=="],"assets":{` + r1 + `}}`,
			wantErr: "onesie: a body for clef carries assets or images, not both",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := assetsToImagesRaw(t, tc.body)
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

func assetsToImagesRaw(t *testing.T, body string) ([]member, error) {
	t.Helper()

	read := readBody(t, body)

	urls, err := read.dataURLs()
	if err != nil {
		return nil, err
	}

	return assetsToImages(read.members, urls)
}
