package onesie_test

import (
	"encoding/json"
	"testing"

	"github.com/frodi-karlsson/onesie-go"
)

func TestPrepareRawBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		body        string
		model       string
		longContext bool
		want        string
	}{
		{
			name:  "should put the default model first in a body that names none",
			body:  `{"state":"s","questions":{}}`,
			model: "jev-latest",
			want:  `{"model":"jev-latest","state":"s","questions":{}}`,
		},
		{
			name:  "should fill an empty object",
			body:  ` {} `,
			model: "jev-latest",
			want:  `{"model":"jev-latest"}`,
		},
		{
			name:  "should fill an empty model in place",
			body:  `{"state":"s","model":"","questions":{}}`,
			model: "jev-latest",
			want:  `{"state":"s","model":"jev-latest","questions":{}}`,
		},
		{
			name:  "should fill a null model in place",
			body:  `{"state":"s","model":null}`,
			model: "jev-latest",
			want:  `{"state":"s","model":"jev-latest"}`,
		},
		{
			name:  "should fill a whitespace model in place",
			body:  `{"model":"  ","state":"s"}`,
			model: "jev-latest",
			want:  `{"model":"jev-latest","state":"s"}`,
		},
		{
			name:  "should leave a body that names a model byte for byte",
			body:  "{\n  \"model\": \"m\",\n  \"state\": 1.50\n}",
			model: "jev-latest",
			want:  "{\n  \"model\": \"m\",\n  \"state\": 1.50\n}",
		},
		{
			name:  "should keep the other members as written",
			body:  `{"state":"a < b  ","n":1.50,"questions":{}}`,
			model: "m",
			want:  `{"model":"m","state":"a < b  ","n":1.50,"questions":{}}`,
		},
		{
			name:  "should keep a large integer and an out of range number digit for digit",
			body:  `{"state":{"id":123456789012345678901234567890,"big":1e400,"small":-0.10000000000000000001}}`,
			model: "m",
			want:  `{"model":"m","state":{"id":123456789012345678901234567890,"big":1e400,"small":-0.10000000000000000001}}`,
		},
		{
			name:        "should keep numbers digit for digit when it adds long context",
			body:        `{"model":"m","n":9007199254740993,"x":1e400}`,
			longContext: true,
			want:        `{"model":"m","n":9007199254740993,"x":1e400,"long_context":true}`,
		},
		{
			name:  "should keep duplicate model keys when the last one names a model",
			body:  `{"model":"","state":"s","model":"m"}`,
			model: "jev-latest",
			want:  `{"model":"","state":"s","model":"m"}`,
		},
		{
			name:  "should replace every model key with one when the last one is blank",
			body:  `{"model":"m","state":"s","model":null}`,
			model: "jev-latest",
			want:  `{"model":"jev-latest","state":"s"}`,
		},
		{
			name:  "should leave a model that is not a string for the server to refuse",
			body:  `{"model":5}`,
			model: "jev-latest",
			want:  `{"model":5}`,
		},
		{
			name: "should leave a blank model when there is no default",
			body: `{"state":"s"}`,
			want: `{"state":"s"}`,
		},
		{
			name:        "should append long context to a body without it",
			body:        `{"model":"m","state":"s"}`,
			longContext: true,
			want:        `{"model":"m","state":"s","long_context":true}`,
		},
		{
			name:        "should turn a body's own false long context true in place",
			body:        `{"model":"m","long_context":false,"state":"s"}`,
			longContext: true,
			want:        `{"model":"m","long_context":true,"state":"s"}`,
		},
		{
			name:        "should collapse duplicate long context keys into one",
			body:        `{"long_context":false,"model":"m","long_context":"yes"}`,
			longContext: true,
			want:        `{"long_context":true,"model":"m"}`,
		},
		{
			name: "should leave a body's own long context when the flag is off",
			body: `{"model":"m","long_context":true}`,
			want: `{"model":"m","long_context":true}`,
		},
		{
			name:        "should fill the model and long context together",
			body:        `{"state":"s"}`,
			model:       "jev-latest",
			longContext: true,
			want:        `{"model":"jev-latest","state":"s","long_context":true}`,
		},
		{
			name:        "should leave an array as it is",
			body:        `[1]`,
			model:       "jev-latest",
			longContext: true,
			want:        `[1]`,
		},
		{
			name:        "should leave a body that is not JSON as it is",
			body:        `{"state":`,
			model:       "jev-latest",
			longContext: true,
			want:        `{"state":`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := onesie.PrepareRawBody(json.RawMessage(tc.body), tc.model, tc.longContext)
			if string(got) != tc.want {
				t.Errorf("body\ngot  %s\nwant %s", got, tc.want)
			}
		})
	}
}
