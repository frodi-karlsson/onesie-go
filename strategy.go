package onesie

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
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

func scanBody(body []byte) bodyScan {
	var scan bodyScan

	// A bytes.Buffer, which jsontext parses in place, so no image is copied.
	dec := jsontext.NewDecoder(bytes.NewBuffer(body), rawOptions...)
	if dec.PeekKind() != '{' {
		return scan
	}

	if _, err := dec.ReadToken(); err != nil {
		return scan
	}

	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return scan
		}

		key := name.String()

		value, err := dec.ReadValue()
		if err != nil {
			return scan
		}

		switch key {
		case "model":
			scan.model = modelName(value)
		case "assets":
			scan.assets = scan.assets || nonEmptyAssets(value)
		case "questions":
			scan.box = scan.box || hasBoxQuestion(value)
		}
	}

	return scan
}

type bodyScan struct {
	model  string
	assets bool
	box    bool
}

func modelName(value jsontext.Value) string {
	var model string
	if json.Unmarshal(value, &model) != nil {
		return ""
	}

	return strings.TrimSpace(model)
}

func nonEmptyAssets(value jsontext.Value) bool {
	switch value.Kind() {
	case 'n':
		return false
	case '{':
		dec := jsontext.NewDecoder(bytes.NewBuffer(value), rawOptions...)
		if _, err := dec.ReadToken(); err != nil {
			return true
		}

		return dec.PeekKind() != '}'
	default:
		return true
	}
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
