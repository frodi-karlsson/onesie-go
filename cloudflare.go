package onesie

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

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
