package onesie

import (
	"errors"
	"testing"
)

func TestRouteRefuse(t *testing.T) {
	t.Parallel()

	const neither = "onesie: the bare provider takes no images or box questions. The typesafe and openrouter providers do"

	tests := []struct {
		name    string
		takes   capabilities
		needs   capabilities
		wantErr string
	}{
		{name: "should take what the route takes", takes: takesEverything(), needs: takesEverything()},
		{name: "should take a request that needs nothing", needs: capabilities{}},
		{name: "should refuse images on a route that takes neither", needs: capabilities{images: true}, wantErr: neither},
		{name: "should refuse a box question on a route that takes neither", needs: capabilities{box: true}, wantErr: neither},
		{
			name:    "should refuse a box question on a route that takes images only",
			takes:   capabilities{images: true},
			needs:   capabilities{images: true, box: true},
			wantErr: "onesie: the bare provider takes no box questions",
		},
		{name: "should take images on a route that takes images only", takes: capabilities{images: true}, needs: capabilities{images: true}},
		{
			name:    "should refuse long context on a route that does not take it",
			takes:   capabilities{images: true, box: true},
			needs:   capabilities{longContext: true},
			wantErr: "onesie: the bare provider takes no long context",
		},
		{
			name:    "should refuse images before long context",
			needs:   capabilities{images: true, longContext: true},
			wantErr: neither,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := route{takes: tc.takes}.refuse("bare", tc.needs)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}

			if !errors.Is(err, ErrValidation) || err.Error() != tc.wantErr {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
