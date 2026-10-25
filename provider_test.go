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
		{name: "should return cloudflare for cloudflare", input: "cloudflare", want: "cloudflare"},
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

		for _, want := range []string{"nope", "typesafe", "openrouter", "berget", "levanto", "cloudflare"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to name %q", err.Error(), want)
			}
		}
	})
}

func TestProviderRefuse(t *testing.T) {
	t.Parallel()

	const neither = "onesie: the levanto provider takes no images or box questions. The typesafe and openrouter providers do"

	everything := onesie.Needs{Images: true, Box: true, LongContext: true}

	tests := []struct {
		name     string
		provider onesie.Provider
		model    string
		needs    onesie.Needs
		wantErr  string
	}{
		{name: "should take everything on typesafe", provider: onesie.TypeSafe(), needs: everything},
		{name: "should take everything on openrouter", provider: onesie.OpenRouter(), needs: everything},
		{name: "should refuse images on levanto", provider: onesie.Levanto(), needs: onesie.Needs{Images: true}, wantErr: neither},
		{
			name: "should refuse images on berget", provider: onesie.Berget(), needs: onesie.Needs{Images: true, Box: true},
			wantErr: "onesie: the berget provider takes no images or box questions. The typesafe and openrouter providers do",
		},
		{name: "should take long context on levanto", provider: onesie.Levanto(), needs: onesie.Needs{LongContext: true}},
		{name: "should read a blank model as the default", provider: onesie.Levanto(), model: "  ", needs: onesie.Needs{Box: true}, wantErr: neither},
		{name: "should take everything on a provider a caller built", provider: onesie.Provider{Name: "mine"}, needs: everything},
		{name: "should take images on cloudflare", provider: onesie.Cloudflare(), needs: onesie.Needs{Images: true}},
		{
			name: "should refuse a box question on cloudflare", provider: onesie.Cloudflare(), model: "clef-flash",
			needs:   onesie.Needs{Images: true, Box: true},
			wantErr: "onesie: the cloudflare provider takes no box questions. The typesafe and openrouter providers do",
		},
		{
			name: "should refuse long context on cloudflare", provider: onesie.Cloudflare(),
			needs:   onesie.Needs{LongContext: true},
			wantErr: "onesie: the cloudflare provider takes no long context. The typesafe provider does",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.provider.Refuse(tc.model, tc.needs)
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
