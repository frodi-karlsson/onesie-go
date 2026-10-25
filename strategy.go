package onesie

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"slices"
	"strings"
)

// CheckImages returns the error a request with images or a Box question gets from this provider on
// its built in default model, or nil when that model reads them.
func (p Provider) CheckImages() error {
	return p.route(p.defaultModel).refuse(p.Name, capabilities{images: true, box: true})
}

func assetsMap(body []byte) ([]byte, []RequestOption, error) {
	return body, []RequestOption{WithRequestHeader(HeaderImages, "true")}, nil
}

func requestNeeds(req Request) capabilities {
	return capabilities{
		images: len(req.Assets) > 0,
		box: slices.ContainsFunc(req.Questions, func(named NamedQuestion) bool {
			_, isBox := named.Question.(Box)

			return isBox
		}),
		longContext: req.LongContext,
	}
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
