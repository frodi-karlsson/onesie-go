//go:build integration

package onesie

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"
)

func TestLiveTextOnlyVercelModels(t *testing.T) {
	t.Parallel()

	t.Run("should list every decision model the gateway catalogue says reads text only", func(t *testing.T) {
		t.Parallel()

		// The catalogue says text only for these, and both answer a request with an image.
		readImagesAnyway := []string{"openai/gpt-6-luna-decisions", "typesafe-ai/jev"}

		var textOnly, exceptions []string

		for _, model := range gatewayCatalogue(t) {
			switch {
			case model.Type != "evaluation" || slices.Contains(model.Modalities.Input, "image"):
			case slices.Contains(readImagesAnyway, model.ID):
				exceptions = append(exceptions, model.ID)
			default:
				textOnly = append(textOnly, model.ID)
			}
		}

		slices.Sort(textOnly)
		slices.Sort(exceptions)

		if listed := slices.Sorted(slices.Values(textOnlyVercelModels)); !slices.Equal(textOnly, listed) {
			t.Errorf("the catalogue's text only decision models are %q, want %q", textOnly, listed)
		}

		if kept := slices.Sorted(slices.Values(readImagesAnyway)); !slices.Equal(exceptions, kept) {
			t.Errorf("the catalogue lists %q of the exceptions %q as text only, so drop the others", exceptions, kept)
		}
	})
}

type catalogueModel struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Modalities struct {
		Input []string `json:"input"`
	} `json:"modalities"`
}

func gatewayCatalogue(t *testing.T) []catalogueModel {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ai-gateway.vercel.sh/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("fetching the gateway catalogue: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the gateway catalogue answered %d", resp.StatusCode)
	}

	var catalogue struct {
		Data []catalogueModel `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&catalogue); err != nil {
		t.Fatalf("decoding the gateway catalogue: %v", err)
	}

	return catalogue.Data
}
