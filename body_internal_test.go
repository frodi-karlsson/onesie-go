package onesie

import (
	"bytes"
	"errors"
	"testing"
)

func TestJsonBodyParsed(t *testing.T) {
	t.Parallel()

	t.Run("should leave a body unread until a reader asks, and give its bytes back as they are", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{ "state" : "s" }`)

		unread := newBody(body)
		if unread.read || unread.object || unread.members != nil {
			t.Fatalf("newBody = %+v, want it unread", unread)
		}

		if out, err := unread.bytes(); err != nil || !bytes.Equal(out, body) {
			t.Errorf("bytes = %s, %v, want the body as it is", out, err)
		}
	})

	t.Run("should keep each value as a slice of the body, so no image is copied", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{"state":"s","assets":{"r":{"data_base64":"AQ=="}}}`)

		parsed := newBody(body).parsed()
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

			parsed := newBody([]byte(tc.body)).parsed()
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

func TestCheckBodySize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		size    int
		wantErr string
	}{
		{name: "should take a body of exactly the limit", size: MaxBodyBytes},
		{
			name: "should refuse a body one byte over the limit", size: MaxBodyBytes + 1,
			wantErr: "onesie: the request body is 33554433 bytes, and the API takes at most 33554432. Send fewer or smaller images",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := checkBodySize(tc.size)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("checkBodySize(%d) = %v, want nil", tc.size, err)
				}

				return
			}

			if err == nil || err.Error() != tc.wantErr || !errors.Is(err, ErrValidation) {
				t.Errorf("checkBodySize(%d) = %v, want %q matching ErrValidation", tc.size, err, tc.wantErr)
			}
		})
	}
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
