package onesie

import (
	"errors"
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
