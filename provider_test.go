package onesie_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/frodi-karlsson/onesie-go"
)

func TestProviderNamed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "should return typesafe for typesafe", input: "typesafe", want: "typesafe"},
		{name: "should return typesafe for an empty name", input: "", want: "typesafe"},
		{name: "should return openrouter for openrouter", input: "openrouter", want: "openrouter"},
		{name: "should return berget for berget", input: "berget", want: "berget"},
		{name: "should return levanto for levanto", input: "levanto", want: "levanto"},
		{name: "should trim the name", input: " openrouter ", want: "openrouter"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			provider, err := onesie.ProviderNamed(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if provider.Name != tc.want {
				t.Errorf("Name = %q, want %q", provider.Name, tc.want)
			}
		})
	}

	t.Run("should reject an unknown name and list the valid ones", func(t *testing.T) {
		t.Parallel()

		_, err := onesie.ProviderNamed("nope")
		if !errors.Is(err, onesie.ErrValidation) {
			t.Fatalf("error got %v, want ErrValidation", err)
		}

		for _, want := range []string{"nope", "typesafe", "openrouter", "berget", "levanto"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to name %q", err.Error(), want)
			}
		}
	})
}

func TestProviderCheckImages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider onesie.Provider
		wantErr  string
	}{
		{name: "should take images on typesafe", provider: onesie.TypeSafe()},
		{name: "should take images on openrouter", provider: onesie.OpenRouter()},
		{name: "should take images on a provider a caller built", provider: onesie.Provider{Name: "mine", BaseURL: "https://x"}},
		{
			name:     "should refuse images on levanto",
			provider: onesie.Levanto(),
			wantErr:  "onesie: the levanto provider takes no images or box questions. The typesafe and openrouter providers do",
		},
		{
			name:     "should refuse images on berget",
			provider: onesie.Berget(),
			wantErr:  "onesie: the berget provider takes no images or box questions. The typesafe and openrouter providers do",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.provider.CheckImages()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}

			if !errors.Is(err, onesie.ErrValidation) || err.Error() != tc.wantErr {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
