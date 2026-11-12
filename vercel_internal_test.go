package onesie

import (
	"testing"
)

func TestWithGatewayCost(t *testing.T) {
	t.Parallel()

	const answers = `"answers":{"q":{"type":"noul","noul":0.9}}`

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "should lift the gateway's cost into the usage",
			body: `{` + answers + `,"usage":{"input_tokens":3,"output_tokens":1},"provider_metadata":{"gateway":{"cost":"0.00001155"}}}`,
			want: `{` + answers + `,"provider_metadata":{"gateway":{"cost":"0.00001155"}},"usage":{"cost":0.00001155,"input_tokens":3,"output_tokens":1}}`,
		},
		{
			name: "should keep a cost the usage already carries",
			body: `{` + answers + `,"usage":{"input_tokens":3,"cost":0.5},"provider_metadata":{"gateway":{"cost":"0.1"}}}`,
			want: `{` + answers + `,"usage":{"input_tokens":3,"cost":0.5},"provider_metadata":{"gateway":{"cost":"0.1"}}}`,
		},
		{
			name: "should leave the cost absent when the gateway's does not parse",
			body: `{` + answers + `,"usage":{"input_tokens":3},"provider_metadata":{"gateway":{"cost":"free"}}}`,
			want: `{` + answers + `,"usage":{"input_tokens":3},"provider_metadata":{"gateway":{"cost":"free"}}}`,
		},
		{
			name: "should leave the cost absent when the gateway's is a number rather than a string",
			body: `{` + answers + `,"usage":{"input_tokens":3},"provider_metadata":{"gateway":{"cost":0.1}}}`,
			want: `{` + answers + `,"usage":{"input_tokens":3},"provider_metadata":{"gateway":{"cost":0.1}}}`,
		},
		{
			name: "should leave the cost absent when it is negative or not finite",
			body: `{` + answers + `,"usage":{"input_tokens":3},"provider_metadata":{"gateway":{"cost":"NaN"}}}`,
			want: `{` + answers + `,"usage":{"input_tokens":3},"provider_metadata":{"gateway":{"cost":"NaN"}}}`,
		},
		{
			name: "should leave a body without usage alone",
			body: `{` + answers + `,"provider_metadata":{"gateway":{"cost":"0.1"}}}`,
			want: `{` + answers + `,"provider_metadata":{"gateway":{"cost":"0.1"}}}`,
		},
		{
			name: "should leave a body that is not an object alone",
			body: `[1,2]`,
			want: `[1,2]`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := withGatewayCost([]byte(tc.body))
			if err != nil || string(got) != tc.want {
				t.Errorf("withGatewayCost = %s, %v, want %s", got, err, tc.want)
			}
		})
	}
}
