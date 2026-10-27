package onesie

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvelope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "should put the body under input with the wire id",
			body: `{"state":"s","model":"typesafe/jev","questions":{}}`,
			want: `{"model":"typesafe/jev","input":{"state":"s","questions":{}}}`,
		},
		{
			name: "should remove every model member",
			body: `{"model":"a","state":"s","model":"b"}`,
			want: `{"model":"typesafe/jev","input":{"state":"s"}}`,
		},
		{
			name: "should keep raw strings as written",
			body: `{"state":"a < b & c","model":"m"}`,
			want: `{"model":"typesafe/jev","input":{"state":"a < b & c"}}`,
		},
		{
			name: "should wrap a body that is not an object as it is",
			body: `[1]`,
			want: `{"model":"typesafe/jev","input":[1]}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := envelope("typesafe/jev")([]byte(tc.body))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if string(got) != tc.want {
				t.Errorf("envelope = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestDropAssets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "should drop empty assets", body: `{"state":"s","assets":{}}`, want: `{"state":"s"}`},
		{name: "should drop null assets", body: `{"state":"s","assets":null}`, want: `{"state":"s"}`},
		{name: "should leave a body without assets as it is", body: `{"state":"s" }`, want: `{"state":"s" }`},
		{name: "should leave a body that is not an object as it is", body: `[1]`, want: `[1]`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := dropAssets([]byte(tc.body))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if string(got) != tc.want {
				t.Errorf("dropAssets = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestChain(t *testing.T) {
	t.Parallel()

	appendByte := func(b byte) func([]byte) ([]byte, error) {
		return func(body []byte) ([]byte, error) { return append(body, b), nil }
	}

	t.Run("should run the steps in order", func(t *testing.T) {
		t.Parallel()

		got, err := chain(appendByte('a'), appendByte('b'))([]byte("x"))
		if err != nil || string(got) != "xab" {
			t.Errorf("chain = %q, %v, want xab", got, err)
		}
	})

	t.Run("should stop at the first error", func(t *testing.T) {
		t.Parallel()

		failing := func([]byte) ([]byte, error) { return nil, errors.New("failed") }

		if _, err := chain(failing, appendByte('b'))([]byte("x")); err == nil || err.Error() != "failed" {
			t.Errorf("error = %v, want failed", err)
		}
	})
}

func TestUnwrapResult(t *testing.T) {
	t.Parallel()

	const answers = `{"model":"m","answers":{},"usage":{"input_tokens":1,"output_tokens":0}}`

	tests := []struct {
		name    string
		body    string
		want    string
		wantErr string
	}{
		{name: "should unwrap clef's one level", body: `{"result":` + answers + `,"success":true}`, want: answers},
		{
			name: "should unwrap jev's two levels",
			body: `{"result":{"state":"Completed","result":` + answers + `},"success":true}`,
			want: answers,
		},
		{
			name:    "should refuse a run that is not completed",
			body:    `{"result":{"state":"Running","result":{}},"success":true}`,
			wantErr: "the cloudflare run is Running",
		},
		{name: "should refuse a body with no result", body: `{"success":true}`, wantErr: "expected a result object"},
		{name: "should refuse a result that is not an object", body: `{"result":[1]}`, wantErr: "expected a result object"},
		{
			name:    "should refuse a completed run with no inner result",
			body:    `{"result":{"state":"Completed"}}`,
			wantErr: "expected a result object",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := unwrapResult([]byte(tc.body))
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}

				return
			}

			if err != nil || string(got) != tc.want {
				t.Errorf("unwrapResult = %s, %v, want %s", got, err, tc.want)
			}
		})
	}
}

func TestCloudflareRoute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		model string
		wire  string
		clef  bool
	}{
		{model: "typesafe/jev", wire: "typesafe/jev"},
		{model: "jev-latest", wire: "jev-latest"},
		{model: "clef", wire: "@cf/cloudflare/clef", clef: true},
		{model: "clef-flash", wire: "@cf/cloudflare/clef-flash", clef: true},
		{model: "clef-omni", wire: "@cf/cloudflare/clef-omni", clef: true},
		{model: "@cf/cloudflare/clef", wire: "@cf/cloudflare/clef", clef: true},
	}

	for _, tc := range tests {
		t.Run("should route "+tc.model, func(t *testing.T) {
			t.Parallel()

			got := cloudflareRoute(tc.model)
			if got.path != "/run" || got.takes != (capabilities{images: true}) {
				t.Fatalf("route = path %q takes %+v, want /run taking images only", got.path, got.takes)
			}

			body := `{"state":"s","model":"x","questions":{},"assets":{}}`
			sent, err := got.encode([]byte(body))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			want := `{"model":"` + tc.wire + `","input":{"state":"s","questions":{},"assets":{}}}`
			if tc.clef {
				want = `{"model":"` + tc.wire + `","input":{"state":"s","questions":{}}}`
			}

			if string(sent) != want {
				t.Errorf("encode = %s, want %s", sent, want)
			}
		})
	}

	t.Run("should send a body naming no model as the default model", func(t *testing.T) {
		t.Parallel()

		sent, err := Cloudflare().route(cloudflareJev).encode([]byte(`{"state":"s","questions":{}}`))
		if err != nil || string(sent) != `{"model":"typesafe/jev","input":{"state":"s","questions":{}}}` {
			t.Errorf("encode = %s, %v, want the default model's envelope", sent, err)
		}
	})
}

func TestDecodeCloudflareModels(t *testing.T) {
	t.Parallel()

	t.Run("should list jev first, then the clef models by short name", func(t *testing.T) {
		t.Parallel()

		body, err := os.ReadFile(filepath.Join("testdata", "cloudflare-models.json"))
		if err != nil {
			t.Fatalf("reading the fixture: %v", err)
		}

		cards, err := decodeCloudflareModels(body)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		names := make([]string, 0, len(cards))
		for _, card := range cards {
			names = append(names, card.Name+" "+card.ReleaseDate)
		}

		want := []string{"typesafe/jev ", "clef 2026-09-29", "clef-omni 2026-10-09", "clef-flash 2026-09-29"}
		if strings.Join(names, ",") != strings.Join(want, ",") {
			t.Errorf("models = %v, want %v", names, want)
		}
	})

	t.Run("should leave out a model that is not clef", func(t *testing.T) {
		t.Parallel()

		cards, err := decodeCloudflareModels([]byte(`{"result":[{"name":"@cf/meta/llama","created_at":"2026-01-01 00:00:00"}]}`))
		if err != nil || len(cards) != 1 {
			t.Errorf("models = %+v, %v, want jev alone", cards, err)
		}
	})

	t.Run("should refuse a body with no list", func(t *testing.T) {
		t.Parallel()

		if _, err := decodeCloudflareModels([]byte(`{"success":false}`)); err == nil {
			t.Error("expected an error, got none")
		}
	})
}
