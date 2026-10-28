package onesie

import (
	"errors"
	"testing"
)

func TestCheckJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "should take a body that is JSON", body: "{\n  \"state\": 1\n}"},
		{name: "should take a JSON value that is not an object", body: `[1]`},
		{name: "should name the fault of a body that ends early", body: `{"state":1,`, want: "onesie: the request body is not JSON: unexpected end of JSON input"},
		{name: "should name the fault of an empty body", want: "onesie: the request body is not JSON: unexpected end of JSON input"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := checkJSON([]byte(tc.body))
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}

			if !errors.Is(err, ErrValidation) || err.Error() != tc.want {
				t.Errorf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
