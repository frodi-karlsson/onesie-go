package onesie

import (
	"bytes"
	"testing"
)

func TestParseBody(t *testing.T) {
	t.Parallel()

	t.Run("should keep each value as a slice of the body, so no image is copied", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{"state":"s","assets":{"r":{"data_base64":"AQ=="}}}`)

		parsed := parseBody(body)
		if !parsed.object || len(parsed.members) != 2 {
			t.Fatalf("parsed = %+v, want two members", parsed)
		}

		at := bytes.Index(body, []byte(`{"r"`))
		if &parsed.members[1].value[0] != &body[at] {
			t.Errorf("the assets value is a copy, want a slice of the body")
		}
	})

	tests := []struct {
		name   string
		body   string
		object bool
		values []string
	}{
		{name: "should read values with space around them", body: `{ "a" : 1 , "b" : [ 2 ] }`, object: true, values: []string{"1", "[ 2 ]"}},
		{name: "should keep duplicate names in order", body: `{"a":1,"a":2}`, object: true, values: []string{"1", "2"}},
		{name: "should read an empty object", body: ` {} `, object: true},
		{name: "should not read an array", body: `[1]`},
		{name: "should not read data after the object", body: `{"a":1} x`},
		{name: "should not read a body cut short", body: `{"a":`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parsed := parseBody([]byte(tc.body))
			if parsed.object != tc.object || len(parsed.members) != len(tc.values) {
				t.Fatalf("parsed = %+v, want object %v with %d members", parsed, tc.object, len(tc.values))
			}

			for i, want := range tc.values {
				if got := string(parsed.members[i].value); got != want {
					t.Errorf("member %d = %s, want %s", i, got, want)
				}
			}

			if out, err := parsed.bytes(); err != nil || string(out) != tc.body {
				t.Errorf("bytes = %s, %v, want the body unchanged", out, err)
			}
		})
	}
}

func TestBodySize(t *testing.T) {
	t.Parallel()

	t.Run("should size a request with images exactly as MarshalBody encodes it", func(t *testing.T) {
		t.Parallel()

		img := validImage(t)
		req := Request{
			State: map[string]any{"a": "image-a", "b": "image-b"}, Model: "m", LongContext: true,
			Questions: Questions{{ID: "q", Question: Noul{Instructions: "q"}}},
			Assets:    Assets{"image-a": img, "image-b": img},
		}

		body, err := MarshalBody(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if size, err := bodySize(req); err != nil || size != len(body) {
			t.Errorf("bodySize = %d, %v, want %d", size, err, len(body))
		}
	})
}

func TestEncodedSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		members []member
	}{
		{name: "should size plain names exactly", members: []member{{name: "state", value: []byte(`"s"`)}, {name: "model", value: []byte(`"m"`)}}},
		{name: "should size a name with a quote and a backslash as it is written", members: []member{{name: `a"b\c`, value: []byte(`1`)}}},
		{name: "should size a name with control characters as it is written", members: []member{{name: "a\nb\x01", value: []byte(`1`)}}},
		{name: "should size an empty list", members: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			written, err := writeMembers(tc.members)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if size, err := encodedSize(tc.members); err != nil || size != len(written) {
				t.Errorf("encodedSize = %d, %v, want %d for %s", size, err, len(written), written)
			}
		})
	}
}
