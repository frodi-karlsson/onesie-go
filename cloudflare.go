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
			encode: encodeClef(wireID),
			decode: unwrapResult,

			describeError: readCloudflareMessage,
			classifyError: classifyBy(cloudflareAuth, unknownModel("No such model")),
		}
	}

	return route{
		path:   "/run",
		takes:  capabilities{images: true},
		encode: envelope(model),
		decode: unwrapResult,

		describeError: readCloudflareMessage,
		classifyError: classifyBy(
			cloudflareAuth, unknownModel("Unknown model"), unknownModel("Model not found"),
			contextTooLong, boxNotServed, countDetail(gatewayForwarded), questionLimit,
		),
	}
}

func readCloudflareMessage(body any) string {
	if fields, isObject := body.(map[string]any); isObject {
		if text := firstError(fields); text != "" {
			return trimCloudflare(text)
		}
	}

	return readMessage(body)
}

func trimCloudflare(text string) string {
	text = withoutCloudflarePrefixes(text)

	// AI Gateway forwards TypeSafe's own error body as the rest of the message.
	if strings.HasPrefix(text, "{") {
		if inner := readMessage(decodeBody([]byte(text))); inner != "" {
			return inner
		}
	}

	return text
}

func withoutCloudflarePrefixes(text string) string {
	for {
		trimmed := strings.TrimPrefix(strings.TrimPrefix(text, "AiError: "), "Ai: ")
		if trimmed == text {
			break
		}

		text = trimmed
	}

	return strings.TrimPrefix(text, "Model execution failed (User Input Error): ")
}

func gatewayForwarded(body any) any {
	fields, isObject := body.(map[string]any)
	if !isObject {
		return nil
	}

	return decodeBody([]byte(withoutCloudflarePrefixes(firstError(fields))))
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

func cloudflareBaseURL(account string) string {
	return "https://api.cloudflare.com/client/v4/accounts/" + account + "/ai"
}

func encodeClef(wireID string) func(jsonBody) ([]byte, error) {
	wrap := envelope(wireID)

	return func(b jsonBody) ([]byte, error) {
		return wrap(withoutAssets(b))
	}
}

func envelope(wireID string) func(jsonBody) ([]byte, error) {
	return func(b jsonBody) ([]byte, error) {
		if b.object {
			b = b.with(slices.DeleteFunc(slices.Clone(b.members), named("model")))
		}

		input, err := b.bytes()
		if err != nil {
			return nil, err
		}

		model, err := json.Marshal(wireID)
		if err != nil {
			return nil, err
		}

		return writeMembers([]member{{name: "model", value: model}, {name: "input", value: input}})
	}
}

func withoutAssets(b jsonBody) jsonBody {
	if !b.object || !slices.ContainsFunc(b.members, named("assets")) {
		return b
	}

	return b.with(slices.DeleteFunc(slices.Clone(b.members), named("assets")))
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

		cards = append(cards, ModelCard{Name: short, Description: model.Description, ReleaseDate: dayOf(model.CreatedAt)})
	}

	return cards, nil
}
