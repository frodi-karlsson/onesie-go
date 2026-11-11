package onesie

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
)

func TestScanMembers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want bodyScan
	}{
		{name: "should see assets", body: `{"state":"r","questions":{},"assets":{"r":{}}}`, want: bodyScan{assets: true}},
		{name: "should see assets that are not an object", body: `{"assets":[1],"questions":{}}`, want: bodyScan{assets: true}},
		{name: "should not see an empty assets object", body: `{"state":"s","questions":{},"assets":{ }}`},
		{name: "should not see null assets", body: `{"state":"s","questions":{},"assets":null}`},
		{name: "should not see a text body", body: `{"state":"s","questions":{"q":{"type":"noul"}}}`},
		{name: "should match the assets name exactly", body: `{"state":"s","questions":{},"Assets":{"r":{}}}`},
		{name: "should read the model", body: `{"model":"clef","questions":{}}`, want: bodyScan{model: "clef"}},
		{name: "should trim the model", body: `{"model":"  clef ","questions":{}}`, want: bodyScan{model: "clef"}},
		{name: "should read a blank model as none", body: `{"model":"   ","questions":{}}`},
		{name: "should read a model that is not a string as none", body: `{"model":1,"questions":{}}`},
		{
			name: "should read the model beside assets and leave the questions alone",
			body: `{"model":"m","assets":{"r":{}},"questions":{"b":{"type":"box"}}}`,
			want: bodyScan{model: "m", assets: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := scanMembers(readBody(t, tc.body).members); got != tc.want {
				t.Errorf("scanMembers = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRequestNeeds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  Request
		want Needs
	}{
		{name: "should need nothing of a text request", req: Request{Questions: Questions{{ID: "q", Question: Noul{}}}}},
		{name: "should need images when the request holds assets", req: Request{Assets: Assets{"r": {}}}, want: Needs{Images: true}},
		{
			name: "should need a box for a box question",
			req:  Request{Questions: Questions{{ID: "q", Question: Noul{}}, {ID: "b", Question: Box{}}}},
			want: Needs{Box: true},
		},
		{
			name: "should need a box for a pointer to a box question",
			req:  Request{Questions: Questions{{ID: "b", Question: &Box{}}}},
			want: Needs{Box: true},
		},
		{
			name: "should need nothing for a nil pointer to a box question",
			req:  Request{Questions: Questions{{ID: "b", Question: (*Box)(nil)}}},
		},
		{name: "should need long context when the request asks for it", req: Request{LongContext: true}, want: Needs{LongContext: true}},
		{
			name: "should need everything at once",
			req:  Request{Assets: Assets{"r": {}}, Questions: Questions{{ID: "b", Question: Box{}}}, LongContext: true},
			want: Needs{Images: true, Box: true, LongContext: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.req.Needs(); got != tc.want {
				t.Errorf("Needs() = %+v, want %+v", got, tc.want)
			}

			if got := requestNeeds(tc.req).asNeeds(); got != tc.want {
				t.Errorf("requestNeeds() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestEncodedPost(t *testing.T) {
	t.Parallel()

	t.Run("should post the encoded body on its route with its needs", func(t *testing.T) {
		t.Parallel()

		via := TypeSafe().route(jevLatest)
		sent := encoded{via: via, body: []byte(`{}`), needs: capabilities{box: true, longContext: true}, images: true}

		var out json.RawMessage

		got := sent.post(&out)
		if got.method != http.MethodPost || got.path != via.path || string(got.payload) != `{}` || got.out != &out ||
			got.needs != (Needs{Box: true, LongContext: true}) || !got.imagesHeader || got.via.path != via.path {
			t.Errorf("post = %+v, want a POST of the encoded body", got)
		}
	})
}

func TestCheckedBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		req   Request
		model string
		want  string
	}{
		{
			name:  "should leave long_context out, since it travels as a header",
			req:   Request{State: "s", Questions: Questions{{ID: "q", Question: Noul{Instructions: "q"}}}, LongContext: true},
			model: "m",
			want:  `{"state":"s","model":"m","questions":{"q":{"type":"noul","instructions":"q"}}}`,
		},
		{
			name:  "should name the model it is given over the request's own",
			req:   Request{State: "s", Model: "own", Questions: Questions{{ID: "q", Question: Noul{Instructions: "q"}}}},
			model: "given",
			want:  `{"state":"s","model":"given","questions":{"q":{"type":"noul","instructions":"q"}}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := checkedBody(tc.req, tc.model)
			if err != nil || string(got.raw) != tc.want {
				t.Errorf("checkedBody = %s, %v, want %s", got.raw, err, tc.want)
			}
		})
	}
}

func TestLayoutParts(t *testing.T) {
	t.Parallel()

	full := Request{
		State: "s", Questions: Questions{{ID: "q", Question: Noul{Instructions: "q"}}}, LongContext: true,
		Assets: Assets{"r": encodedImage("png", []byte{1})},
	}

	t.Run("should write every member the body order names", func(t *testing.T) {
		t.Parallel()

		var names []string
		for _, part := range wholeBody.parts(full, "m") {
			names = append(names, part.name)
		}

		if !slices.Equal(names, bodyOrder) {
			t.Errorf("parts = %v, want %v", names, bodyOrder)
		}
	})
}
