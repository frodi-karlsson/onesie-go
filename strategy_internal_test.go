package onesie

import "testing"

func TestBodyCarriesImages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "should see assets", body: `{"state":"r","questions":{},"assets":{"r":{}}}`, want: true},
		{name: "should see assets that are not an object", body: `{"assets":[1],"questions":{}}`, want: true},
		{name: "should see a box question", body: `{"state":"s","questions":{"m":{"type":"box"}}}`, want: true},
		{name: "should not see an empty assets object", body: `{"state":"s","questions":{},"assets":{ }}`},
		{name: "should not see null assets", body: `{"state":"s","questions":{},"assets":null}`},
		{name: "should not see a text body", body: `{"state":"s","questions":{"q":{"type":"noul"}}}`},
		{name: "should match the assets name exactly", body: `{"state":"s","questions":{},"Assets":{"r":{}}}`},
		{name: "should not see a top level that is not an object", body: `["assets"]`},
		{name: "should not see a body that does not parse", body: `{"assets":`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := bodyCarriesImages([]byte(tc.body)); got != tc.want {
				t.Errorf("bodyCarriesImages = %v, want %v", got, tc.want)
			}
		})
	}
}
