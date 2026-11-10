package onesie

import (
	"errors"
	"testing"
)

func TestReadRawBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		members int
		wantErr string
	}{
		{name: "should read the members of an object", body: "{\n  \"state\": 1, \"model\": \"m\"\n}", members: 2},
		{name: "should refuse a body that ends early", body: `{"state":1,`, wantErr: "onesie: the request body is not JSON: unexpected EOF at offset 11"},
		{name: "should refuse a fault outside every member by its offset", body: `x`, wantErr: "onesie: the request body is not JSON: invalid character 'x' at start of value at offset 0"},
		{name: "should refuse invalid UTF-8 at its pointer", body: "{\"state\":\"\xff\"}", wantErr: "onesie: the request body is not JSON: invalid UTF-8 at /state"},
		{name: "should refuse a lone surrogate, which json.Valid took", body: `{"state":"\ud800"}`, wantErr: "onesie: the request body is not JSON: invalid surrogate pair `\\ud800\"}` in string at /state"},
		{name: "should refuse an empty body", body: " ", wantErr: "onesie: the request body is empty"},
		{name: "should refuse a JSON value that is not an object", body: `[1]`, wantErr: "onesie: the request body must be a JSON object, got an array"},
		{name: "should refuse data after the object", body: `{"state":1} {}`, wantErr: "onesie: the request body has data after its object"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := readRawBody([]byte(tc.body))
			if tc.wantErr == "" {
				if err != nil || len(got.members) != tc.members {
					t.Errorf("readRawBody = %d members, %v, want %d", len(got.members), err, tc.members)
				}

				return
			}

			if !errors.Is(err, ErrValidation) || err.Error() != tc.wantErr {
				t.Errorf("readRawBody error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
