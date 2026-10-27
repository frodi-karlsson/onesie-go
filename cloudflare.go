package onesie

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

func cloudflareRoute(model string) route {
	if wireID, isClef := clefWireID(model); isClef {
		return route{
			path:   "/run",
			takes:  capabilities{images: true},
			images: assetsToImages,
			encode: chain(dropAssets, envelope(wireID)),
			decode: unwrapResult,
		}
	}

	return route{
		path:   "/run",
		takes:  capabilities{images: true},
		images: keepAssets,
		encode: envelope(model),
		decode: unwrapResult,
	}
}

func clefWireID(model string) (string, bool) {
	switch {
	case strings.HasPrefix(model, "@cf/"):
		return model, true
	case strings.HasPrefix(model, "clef"):
		return "@cf/cloudflare/" + model, true
	default:
		return "", false
	}
}

func keepAssets(body []byte) ([]byte, []RequestOption, error) {
	return body, nil, nil
}

func cloudflareBaseURL(account string) string {
	return "https://api.cloudflare.com/client/v4/accounts/" + account + "/ai"
}

func envelope(wireID string) func([]byte) ([]byte, error) {
	return func(body []byte) ([]byte, error) {
		input := body

		if members, readable := objectMembers(body); readable {
			kept, err := writeMembers(slices.DeleteFunc(members, func(m member) bool { return m.name == "model" }))
			if err != nil {
				return nil, err
			}

			input = kept
		}

		model, err := json.Marshal(wireID)
		if err != nil {
			return nil, err
		}

		return writeMembers([]member{{name: "model", value: model}, {name: "input", value: input}})
	}
}

func dropAssets(body []byte) ([]byte, error) {
	members, readable := objectMembers(body)
	if !readable || !slices.ContainsFunc(members, isAssets) {
		return body, nil
	}

	return writeMembers(slices.DeleteFunc(members, isAssets))
}

func isAssets(m member) bool {
	return m.name == "assets"
}

func chain(steps ...func([]byte) ([]byte, error)) func([]byte) ([]byte, error) {
	return func(body []byte) ([]byte, error) {
		for _, step := range steps {
			next, err := step(body)
			if err != nil {
				return nil, err
			}

			body = next
		}

		return body, nil
	}
}

func unwrapResult(body []byte) ([]byte, error) {
	result, err := resultObject(body)
	if err != nil {
		return nil, err
	}

	var run struct {
		State *string `json:"state"`
	}

	if err = json.Unmarshal(result, &run); err != nil {
		return nil, err
	}

	// AI Gateway reports a run, whose own result holds the answers. Workers AI answers directly.
	if run.State == nil {
		return result, nil
	}

	if *run.State != "Completed" {
		return nil, fmt.Errorf("the cloudflare run is %s", *run.State)
	}

	return resultObject(result)
}

func resultObject(body []byte) ([]byte, error) {
	var wire struct {
		Result json.RawMessage `json:"result"`
	}

	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}

	if trimmed := bytes.TrimSpace(wire.Result); len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, errors.New("expected a result object")
	}

	return wire.Result, nil
}

func decodeCloudflareModels(body []byte) ([]ModelCard, error) {
	var wire struct {
		Result []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			CreatedAt   string `json:"created_at"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}

	if wire.Result == nil {
		return nil, errors.New("expected a models list")
	}

	// Jev is served through AI Gateway, so Workers AI's search never lists it.
	cards := []ModelCard{{
		Name:        cloudflareJev,
		Description: "TypeSafe's Jev through AI Gateway, billed from AI Gateway credits",
	}}

	for _, model := range wire.Result {
		short, found := strings.CutPrefix(model.Name, "@cf/cloudflare/")
		if !found || !strings.HasPrefix(short, "clef") {
			continue
		}

		date, _, _ := strings.Cut(model.CreatedAt, " ")
		cards = append(cards, ModelCard{Name: short, Description: model.Description, ReleaseDate: date})
	}

	return cards, nil
}
