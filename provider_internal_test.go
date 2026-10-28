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

	t.Run("should leave the date empty for a created of zero", func(t *testing.T) {
		t.Parallel()

		cards, err := decodeOpenRouterModels([]byte(`{"data":[{"id":"x","created":0}]}`))
		if err != nil || len(cards) != 1 || cards[0].ReleaseDate != "" {
			t.Errorf("cards = %+v, %v, want one card with no date", cards, err)
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

func TestDecodeTypeSafeModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		date string
		want string
	}{
		{name: "should keep a day as it is", date: "2026-08-01", want: "2026-08-01"},
		{name: "should cut a timestamp to its day", date: "2026-08-01 12:00:00", want: "2026-08-01"},
		{name: "should leave a date that does not parse empty", date: "08/01/2026"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cards, err := decodeTypeSafeModels([]byte(`{"models":[{"name":"jev-latest","release_date":"` + tc.date + `"}]}`))
			if err != nil || len(cards) != 1 || cards[0].ReleaseDate != tc.want {
				t.Errorf("cards = %+v, %v, want one card dated %q", cards, err, tc.want)
			}
		})
	}
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

	t.Run("should write every date as a day", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			date string
			want string
		}{
			{name: "should keep a day as it is", date: "2026-09-23", want: "2026-09-23"},
			{name: "should cut a timestamp to its day", date: "2026-09-23T10:00:00Z", want: "2026-09-23"},
			{name: "should leave a date that does not parse empty", date: "soon"},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				body := `{"data":[{"id":"x","model_type":"system-one","release_date":"` + tc.date + `"}]}`

				cards, err := decodeBergetModels([]byte(body))
				if err != nil || len(cards) != 1 || cards[0].ReleaseDate != tc.want {
					t.Errorf("cards = %+v, %v, want one card dated %q", cards, err, tc.want)
				}
			})
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
}
