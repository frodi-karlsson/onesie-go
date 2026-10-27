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
	if err := p.check(); err != nil {
		return err
	}

	model = strings.TrimSpace(model)
	if model == "" {
		model = p.defaultModel
	}

	return p.route(model).refuse(p.name, model, capabilities{
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
	if err := p.check(); err != nil {
		return nil, err
	}

	sent, err := encodeRequest(p, req, false)

	return sent.body, err
}

func encodeRequest(p Provider, req Request, longContext bool) (encoded, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = p.defaultModel
	}

	via := p.route(model)
	needs := requestNeeds(req)
	needs.longContext = needs.longContext || longContext

	if err := via.refuse(p.name, model, needs); err != nil {
		return encoded{}, err
	}

	// Long context travels as a header, so the body never carries it.
	body, err := MarshalBody(Request{State: req.State, Model: model, Questions: req.Questions, Assets: req.Assets})
	if err != nil {
		return encoded{}, unencodable(err)
	}

	return encodeBody(via, needs, parseBody(body))
}

type encoded struct {
	via    route
	body   []byte
	needs  capabilities
	images bool
}

func encodeBody(via route, needs capabilities, b jsonBody) (encoded, error) {
	withImages := needs.images || needs.box

	if withImages && via.images != nil {
		members, err := via.images(b.members)
		if err != nil {
			return encoded{}, err
		}

		b = b.with(members)
	}

	sent, err := via.encode(b)
	if err != nil {
		return encoded{}, err
	}

	return encoded{via: via, body: sent, needs: needs, images: withImages && via.imagesHeader}, nil
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

func scanMembers(members []member) bodyScan {
	var scan bodyScan

	for _, m := range members {
		switch m.name {
		case "model":
			scan.model = modelName(m.value)
		case "assets":
			scan.assets = scan.assets || nonEmptyAssets(m.value)
		case "questions":
			scan.box = scan.box || hasBoxQuestion(m.value)
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
		dec := jsontext.NewDecoder(bytes.NewBuffer(value), rawOptions()...)
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
