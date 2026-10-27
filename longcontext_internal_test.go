package onesie

import (
	"errors"
	"testing"
)

func TestLongContextOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		want    bool
		wantErr string
	}{
		{name: "should read no key as off", body: `{"state":"s"}`},
		{name: "should read true as on", body: `{"long_context":true}`, want: true},
		{name: "should read false as off", body: `{"long_context":false}`},
		{name: "should refuse a value that is not true or false", body: `{"long_context":"yes"}`, wantErr: "onesie: long_context must be true or false"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := longContextOf(parseBody([]byte(tc.body)).members)
			if tc.wantErr != "" {
				if !errors.Is(err, ErrValidation) || err.Error() != tc.wantErr {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}

				return
			}

			if err != nil || got != tc.want {
				t.Errorf("longContextOf = %v, %v, want %v", got, err, tc.want)
			}
		})
	}
}

func TestWithoutLongContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "should leave a body without the key as it is", body: `{ "state" : "s" }`, want: `{ "state" : "s" }`},
		{name: "should take the key out", body: `{"state":"xé <&>","long_context":true,"n":1.50e3}`, want: `{"state":"xé <&>","n":1.50e3}`},
		{name: "should leave the key inside a string alone", body: `{"state":"\"long_context\""}`, want: `{"state":"\"long_context\""}`},
		{name: "should leave a body that is not an object alone", body: `["long_context"]`, want: `["long_context"]`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := withoutLongContext(parseBody([]byte(tc.body))).bytes()
			if err != nil || string(got) != tc.want {
				t.Errorf("withoutLongContext = %s, %v, want %s", got, err, tc.want)
			}
		})
	}
}
