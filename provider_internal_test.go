package onesie

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeOpenRouterModels(t *testing.T) {
	t.Parallel()

	t.Run("should map id, description and created from the captured listing", func(t *testing.T) {
		t.Parallel()

		body, err := os.ReadFile(filepath.Join("testdata", "openrouter-models.json"))
		if err != nil {
			t.Fatalf("reading the fixture: %v", err)
		}

		cards, err := decodeOpenRouterModels(body)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(cards) != 2 {
			t.Fatalf("got %d models, want 2", len(cards))
		}

		latest := cards[0]
		if latest.Name != "~typesafe/jev-latest" {
			t.Errorf("Name = %q, want ~typesafe/jev-latest", latest.Name)
		}

		if !strings.HasPrefix(latest.Description, "This model always redirects") {
			t.Errorf("Description = %q, want the listing's description", latest.Description)
		}

		if latest.ReleaseDate != "2026-09-18" {
			t.Errorf("ReleaseDate = %q, want 2026-09-18", latest.ReleaseDate)
		}
	})

	t.Run("should fail when data is absent", func(t *testing.T) {
		t.Parallel()

		_, err := decodeOpenRouterModels([]byte(`{"models":[]}`))
		if err == nil || !strings.Contains(err.Error(), "expected a models list") {
			t.Errorf("error = %v, want expected a models list", err)
		}
	})

	t.Run("should return an empty list for empty data", func(t *testing.T) {
		t.Parallel()

		cards, err := decodeOpenRouterModels([]byte(`{"data":[]}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cards == nil || len(cards) != 0 {
			t.Errorf("cards = %#v, want an empty slice", cards)
		}
	})
}

func TestDecodeBergetModels(t *testing.T) {
	t.Parallel()

	t.Run("should keep only the system-one models from the captured listing", func(t *testing.T) {
		t.Parallel()

		body, err := os.ReadFile(filepath.Join("testdata", "berget-models.json"))
		if err != nil {
			t.Fatalf("reading the fixture: %v", err)
		}

		cards, err := decodeBergetModels(body)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := []ModelCard{
			{
				Name:        "Qwen/Qwen3.5-2B",
				Description: "aliases systemone, systemone-qwen3.5-2b",
				ReleaseDate: "2026-09-23",
			},
			{
				Name:        "convaiinnovations/laya",
				Description: "aliases systemone-laya, laya-latest",
				ReleaseDate: "2026-09-24",
			},
		}

		if len(cards) != len(want) {
			t.Fatalf("cards = %+v, want %+v", cards, want)
		}

		for i := range want {
			if cards[i] != want[i] {
				t.Errorf("card %d = %+v, want %+v", i, cards[i], want[i])
			}
		}
	})

	t.Run("should leave the description empty for a model with no aliases", func(t *testing.T) {
		t.Parallel()

		cards, err := decodeBergetModels([]byte(`{"data":[{"id":"x","model_type":"system-one","aliases":[]}]}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(cards) != 1 || cards[0] != (ModelCard{Name: "x"}) {
			t.Errorf("cards = %+v, want one card named x", cards)
		}
	})

	t.Run("should fail when data is absent", func(t *testing.T) {
		t.Parallel()

		_, err := decodeBergetModels([]byte(`{"models":[]}`))
		if err == nil || !strings.Contains(err.Error(), "expected a models list") {
			t.Errorf("error = %v, want expected a models list", err)
		}
	})

	t.Run("should return an empty list when no model is system-one", func(t *testing.T) {
		t.Parallel()

		cards, err := decodeBergetModels([]byte(`{"data":[{"id":"mistral","model_type":"text"}]}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cards == nil || len(cards) != 0 {
			t.Errorf("cards = %#v, want an empty slice", cards)
		}
	})
}

func TestProviderRoute(t *testing.T) {
	t.Parallel()

	images := capabilities{images: true, box: true, longContext: true}
	text := capabilities{longContext: true}

	tests := []struct {
		name     string
		provider Provider
		want     capabilities
	}{
		{name: "should give typesafe one route that takes everything", provider: TypeSafe(), want: images},
		{name: "should give openrouter one route that takes everything", provider: OpenRouter(), want: images},
		{name: "should give berget one route that takes long context only", provider: Berget(), want: text},
		{name: "should give levanto one route that takes long context only", provider: Levanto(), want: text},
	}

	for _, tc := range tests {
		for _, model := range []string{"", tc.provider.defaultModel, "some-other-model"} {
			t.Run(tc.name+" for model "+model, func(t *testing.T) {
				t.Parallel()

				got := tc.provider.route(model)
				if got.path != systemOnePath || got.takes != tc.want {
					t.Errorf("route(%q) = path %q takes %+v, want path %q takes %+v",
						model, got.path, got.takes, systemOnePath, tc.want)
				}

				if got.encode == nil || got.decode == nil {
					t.Errorf("route(%q) has a nil encode or decode", model)
				}
			})
		}
	}

	t.Run("should take everything on a provider built outside the package, with every step", func(t *testing.T) {
		t.Parallel()

		got := (Provider{Name: "mine"}).route("m")
		if got.takes != takesEverything() {
			t.Errorf("takes = %+v, want everything", got.takes)
		}

		if got.images == nil || got.encode == nil || got.decode == nil {
			t.Fatal("the route has a nil step")
		}

		body := []byte(`{"state":"s"}`)

		sent, extra, err := got.images(body)
		if err != nil || string(sent) != string(body) || extra != nil {
			t.Errorf("images = %s, %v, %v, want the body unchanged and no options", sent, extra, err)
		}

		encoded, err := got.encode(body)
		if err != nil || string(encoded) != string(body) {
			t.Errorf("encode = %s, %v, want the body unchanged", encoded, err)
		}
	})
}
