package onesie

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"slices"
	"strings"
)

// Refuse returns the error a request with these needs gets from this provider for model, or nil.
// An empty model means the provider's built in default.
func (p Provider) Refuse(model string, needs Needs) error {
	model = strings.TrimSpace(model)
	if model == "" {
		model = p.defaultModel
	}

	return p.route(model).refuse(p.Name, model, capabilities{
		images:      needs.Images,
		box:         needs.Box,
		longContext: needs.LongContext,
	})
}

// Needs is what a request asks of the model it is sent to.
type Needs struct {
	Images      bool
	Box         bool
	LongContext bool
}

// Encode refuses a request this provider's route does not take, then returns the body SystemOne
// sends, without the headers it adds. A blank model is the built in default. It does not validate.
func (p Provider) Encode(req Request) ([]byte, error) {
	_, body, _, err := encodeRequest(p, req, false)

	return body, err
}

func encodeRequest(p Provider, req Request, headerLongContext bool) (route, []byte, []RequestOption, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = p.defaultModel
	}

	via := p.route(model)
	needs := requestNeeds(req)
	needs.longContext = needs.longContext || headerLongContext

	if err := via.refuse(p.Name, model, needs); err != nil {
		return route{}, nil, nil, err
	}

	// Long context travels as a header, so the body never carries it.
	body, err := MarshalBody(Request{State: req.State, Model: model, Questions: req.Questions, Assets: req.Assets})
	if err != nil {
		return route{}, nil, nil, err
	}

	sent, extra, err := encodeBody(via, needs, body)
	if err != nil {
		return route{}, nil, nil, err
	}

	return via, sent, extra, nil
}

func encodeBody(via route, needs capabilities, body []byte) ([]byte, []RequestOption, error) {
	var extra []RequestOption

	if needs.images || needs.box {
		sent, added, err := via.images(body)
		if err != nil {
			return nil, nil, err
		}

		body, extra = sent, added
	}

	encoded, err := via.encode(body)
	if err != nil {
		return nil, nil, err
	}

	return encoded, extra, nil
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
