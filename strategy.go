package onesie

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"slices"
)

// CheckImages returns the error a request with images or a Box question gets from this provider,
// or nil when the provider reads them.
func (p Provider) CheckImages() error {
	if !p.noImages {
		return nil
	}

	return &ValidationError{Message: fmt.Sprintf(
		"the %s provider takes no images or box questions. The typesafe and openrouter providers do", p.Name)}
}

func (p Provider) imageSender() func(body []byte) ([]byte, []RequestOption, error) {
	if p.sendImages == nil {
		return assetsMap
	}

	return p.sendImages
}

func assetsMap(body []byte) ([]byte, []RequestOption, error) {
	return body, []RequestOption{WithRequestHeader(HeaderImages, "true")}, nil
}

func requestCarriesImages(req Request) bool {
	if len(req.Assets) > 0 {
		return true
	}

	return slices.ContainsFunc(req.Questions, func(named NamedQuestion) bool {
		_, isBox := named.Question.(Box)

		return isBox
	})
}

func bodyCarriesImages(body []byte) bool {
	dec := jsontext.NewDecoder(bytes.NewReader(body), rawOptions...)
	if dec.PeekKind() != '{' {
		return false
	}

	if _, err := dec.ReadToken(); err != nil {
		return false
	}

	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return false
		}

		key := name.String()

		value, err := dec.ReadValue()
		if err != nil {
			return false
		}

		if key == "assets" && !emptyAssets(value) || key == "questions" && hasBoxQuestion(value) {
			return true
		}
	}

	return false
}

func emptyAssets(value jsontext.Value) bool {
	var assets map[string]json.RawMessage

	return json.Unmarshal(value, &assets) == nil && len(assets) == 0
}

func hasBoxQuestion(value jsontext.Value) bool {
	var questions map[string]struct {
		Type string `json:"type"`
	}

	if json.Unmarshal(value, &questions) != nil {
		return false
	}

	for _, question := range questions {
		if question.Type == "box" {
			return true
		}
	}

	return false
}
