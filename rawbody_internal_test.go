package onesie

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCheckJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		refused    bool
		wantOffset int64
	}{
		{name: "should take a body that is JSON", body: "{\n  \"state\": 1\n}"},
		{name: "should take a JSON value that is not an object", body: `[1]`},
		{name: "should refuse a body that ends early, with the syntax error behind it", body: `{"state":1,`, refused: true, wantOffset: 11},
		{name: "should refuse an empty body, with the syntax error behind it", refused: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := checkJSON([]byte(tc.body))
			if !tc.refused {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}

			var syntax *json.SyntaxError
			if !errors.Is(err, ErrValidation) || !strings.HasPrefix(err.Error(), "onesie: the request body is not JSON: ") ||
				!errors.As(err, &syntax) || syntax.Offset != tc.wantOffset {
				t.Errorf("error = %v, want a refusal that the body is not JSON, from a syntax error at offset %d", err, tc.wantOffset)
			}
		})
	}
}
