package onesie

import (
	"errors"
	"testing"
)

func TestTakeLongContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		want    string
		wantOn  bool
		wantErr string
	}{
		{name: "should leave a body without the key as it is", body: `{ "state" : "s" }`, want: `{ "state" : "s" }`},
		{
			name:   "should take true out and turn long context on",
			body:   `{"state":"xé <&>","long_context":true,"n":1.50e3}`,
			want:   `{"state":"xé <&>","n":1.50e3}`,
			wantOn: true,
		},
		{name: "should take false out and leave long context off", body: `{"long_context":false,"state":"s"}`, want: `{"state":"s"}`},
		{
			name:   "should take every repeated member out",
			body:   `{"long_context":false,"state":"s","long_context":true}`,
			want:   `{"state":"s"}`,
			wantOn: true,
		},
		{name: "should leave the key inside a string alone", body: `{"state":"\"long_context\""}`, want: `{"state":"\"long_context\""}`},
		{name: "should leave a body that is not an object alone", body: `["long_context"]`, want: `["long_context"]`},
		{name: "should refuse a value that is not true or false", body: `{"long_context":"yes"}`, wantErr: "long_context must be true or false"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, on, err := takeLongContext(parseBody([]byte(tc.body)))
			if tc.wantErr != "" {
				if !errors.Is(err, ErrValidation) || err.Error() != "onesie: "+tc.wantErr {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}

				return
			}

			out, writeErr := got.bytes()
			if err != nil || writeErr != nil {
				t.Fatalf("unexpected error: %v, %v", err, writeErr)
			}

			if string(out) != tc.want || on != tc.wantOn {
				t.Errorf("got %s, %v, want %s, %v", out, on, tc.want, tc.wantOn)
			}
		})
	}
}
