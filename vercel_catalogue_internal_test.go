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

		var textOnly []string

		for _, model := range gatewayCatalogue(t) {
			if model.Type == "evaluation" && !slices.Contains(model.Modalities.Input, "image") && !slices.Contains(readImagesAnyway, model.ID) {
				textOnly = append(textOnly, model.ID)
			}
		}

		slices.Sort(textOnly)

		if !slices.Equal(textOnly, textOnlyVercelModels) {
			t.Errorf("the catalogue's text only decision models are %q, want %q", textOnly, textOnlyVercelModels)
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
