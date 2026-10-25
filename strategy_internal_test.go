package onesie

import "testing"

func TestScanBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want bodyScan
	}{
		{name: "should see assets", body: `{"state":"r","questions":{},"assets":{"r":{}}}`, want: bodyScan{assets: true}},
		{name: "should see assets that are not an object", body: `{"assets":[1],"questions":{}}`, want: bodyScan{assets: true}},
		{name: "should see a box question", body: `{"state":"s","questions":{"m":{"type":"box"}}}`, want: bodyScan{box: true}},
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
			name: "should read the model beside assets and a box question",
			body: `{"model":"m","assets":{"r":{}},"questions":{"b":{"type":"box"}}}`,
			want: bodyScan{model: "m", assets: true, box: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := scanBody([]byte(tc.body)); got != tc.want {
				t.Errorf("scanBody = %+v, want %+v", got, tc.want)
			}
		})
	}
}
