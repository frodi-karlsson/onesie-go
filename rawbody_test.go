package onesie_test

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/frodi-karlsson/onesie-go"
)

func TestProviderPrepareRaw(t *testing.T) {
	t.Parallel()

	const asset = `"assets":{"r":{"format":"png","data_base64":"AQ=="}}`

	longContext := []onesie.RequestOption{onesie.WithLongContext()}

	tests := []struct {
		name     string
		provider onesie.Provider
		body     string
		model    string
		opts     []onesie.RequestOption
		want     string
		wantErr  string
	}{
		{name: "should put the default model first in a body that names none", body: `{"state":"s","questions":{}}`, model: "jev-latest", want: `{"model":"jev-latest","state":"s","questions":{}}`},
		{name: "should fill an empty object", body: ` {} `, model: "jev-latest", want: `{"model":"jev-latest"}`},
		{name: "should fill an empty model in place", body: `{"state":"s","model":"","questions":{}}`, model: "jev-latest", want: `{"state":"s","model":"jev-latest","questions":{}}`},
		{name: "should fill a null model in place", body: `{"state":"s","model":null}`, model: "jev-latest", want: `{"state":"s","model":"jev-latest"}`},
		{name: "should fill a blank model with the provider's own default", provider: onesie.Levanto(), body: `{"state":"s"}`, want: `{"model":"sage-latest","state":"s"}`},
		{name: "should leave a body that names a model byte for byte", body: "{\n  \"model\": \"m\",\n  \"state\": 1.50\n}", model: "jev-latest", want: "{\n  \"model\": \"m\",\n  \"state\": 1.50\n}"},
		{
			name: "should keep a large integer and an out of range number digit for digit", model: "m",
			body: `{"state":{"id":123456789012345678901234567890,"big":1e400,"small":-0.10000000000000000001}}`,
			want: `{"model":"m","state":{"id":123456789012345678901234567890,"big":1e400,"small":-0.10000000000000000001}}`,
		},
		{name: "should keep the last of repeated keys where it stands", body: `{"model":"","state":"s","model":"m"}`, model: "jev-latest", want: `{"state":"s","model":"m"}`},
		{name: "should fill a model whose last key is blank", body: `{"model":"m","state":"s","model":null}`, model: "jev-latest", want: `{"state":"s","model":"jev-latest"}`},
		{name: "should leave a model that is not a string for the server to refuse", body: `{"model":5}`, model: "jev-latest", want: `{"model":5}`},
		{name: "should append long context for WithLongContext", body: `{"model":"m","state":"s"}`, opts: longContext, want: `{"model":"m","state":"s","long_context":true}`},
		{name: "should turn a body's false long context true in place", body: `{"model":"m","long_context":false,"state":"s"}`, opts: longContext, want: `{"model":"m","long_context":true,"state":"s"}`},
		{name: "should keep the last of repeated long context keys", body: `{"long_context":true,"model":"m","long_context":false}`, want: `{"model":"m","long_context":false}`},
		{name: "should keep a body's own long context without the option", body: `{"model":"m","long_context":true}`, want: `{"model":"m","long_context":true}`},
		{name: "should refuse a long context that is not true or false", body: `{"model":"m","long_context":"yes"}`, wantErr: "onesie: long_context must be true or false"},
		{name: "should leave an array as it is", body: `[1]`, model: "jev-latest", want: `[1]`},
		{name: "should leave a body that is not JSON as it is", body: `{"state":`, want: `{"state":`},
		{
			name: "should refuse images the provider does not take", provider: onesie.Levanto(),
			body:    `{"state":"r","questions":{},` + asset + `}`,
			wantErr: "onesie: the levanto provider takes no images. The typesafe, openrouter and cloudflare providers do",
		},
		{
			name:    "should refuse an image a send refuses",
			body:    `{"state":"r","questions":{},"assets":{"r":{"format":"gif","data_base64":"AQ=="}}}`,
			wantErr: "onesie: image 'r' has format 'gif', and an image must be png or jpg",
		},
		{
			name: "should refuse long context the provider does not take", provider: onesie.Cloudflare(), body: `{"state":"s"}`, opts: longContext,
			wantErr: "onesie: the cloudflare provider takes no long context. The typesafe, openrouter, berget and levanto providers do",
		},
		{
			name:    "should refuse a raw score over the levels limit",
			body:    `{"state":"s","questions":{"rate":{"type":"score","instructions":"x","criteria":["0","1","2","3","4","5","6","7","8","9","10"]}}}`,
			wantErr: `onesie: score question "rate" has 11 levels, and at most 10 are allowed`,
		},
		{
			name: "should take a raw score at the levels limit",
			body: `{"model":"m","questions":{"rate":{"type":"score","criteria":["0","1","2","3","4","5","6","7","8","9"]}}}`,
			want: `{"model":"m","questions":{"rate":{"type":"score","criteria":["0","1","2","3","4","5","6","7","8","9"]}}}`,
		},
		{
			name:    "should refuse a raw choice over the options limit",
			body:    `{"state":"s","questions":{"pick":{"type":"choice","criteria":` + manyOptions(256) + `}}}`,
			wantErr: `onesie: choice question "pick" has 256 options, and at most 255 are allowed`,
		},
		{name: "should refuse a failing option", body: `{}`, opts: []onesie.RequestOption{onesie.WithRequestAttemptTimeout(0)}, wantErr: "onesie: attempt timeout must be positive"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			provider := tc.provider
			if provider.Name() == "" {
				provider = onesie.TypeSafe()
			}

			got, err := provider.PrepareRaw(json.RawMessage(tc.body), tc.model, tc.opts...)
			if tc.wantErr != "" {
				if !errors.Is(err, onesie.ErrValidation) || err.Error() != tc.wantErr {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}

				return
			}

			if err != nil || string(got) != tc.want {
				t.Errorf("PrepareRaw = %s, %v, want %s", got, err, tc.want)
			}
		})
	}

	t.Run("should change nothing in a prepared body", func(t *testing.T) {
		t.Parallel()

		for _, body := range []string{`{"model":"","state":"s","model":"m","long_context":false}`, `{"state":"s"}`, `[1]`} {
			once, err := onesie.TypeSafe().PrepareRaw(json.RawMessage(body), "", longContext...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			twice, err := onesie.TypeSafe().PrepareRaw(once, "", longContext...)
			if err != nil || string(twice) != string(once) {
				t.Errorf("PrepareRaw twice = %s, %v, want %s", twice, err, once)
			}
		}
	})

	t.Run("should refuse a zero provider", func(t *testing.T) {
		t.Parallel()

		var zero onesie.Provider

		if _, err := zero.PrepareRaw(json.RawMessage(`{}`), ""); !errors.Is(err, onesie.ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}
	})
}

func TestClientPrepareRaw(t *testing.T) {
	t.Parallel()

	t.Run("should fill the client's default model", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestClient(t, "https://api.example", onesie.WithDefaultModel("custom"))

		got, err := client.PrepareRaw(json.RawMessage(`{"state":"s"}`))
		if err != nil || string(got) != `{"model":"custom","state":"s"}` {
			t.Errorf("PrepareRaw = %s, %v, want the client's model", got, err)
		}
	})
}

func manyOptions(n int) string {
	names := make([]string, n)
	for i := range names {
		names[i] = `"option ` + strconv.Itoa(i) + `":"x"`
	}

	return "{" + strings.Join(names, ",") + "}"
}
