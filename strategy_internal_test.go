package onesie

import (
	"encoding/json"
	"net/http"
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
		{name: "should not see a top level that is not an object", body: `["assets"]`},
		{name: "should not see a body that does not parse", body: `{"assets":`},
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

			if got := scanMembers(parseBody([]byte(tc.body)).members); got != tc.want {
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
