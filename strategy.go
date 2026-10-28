package onesie

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"net/http"
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

	return p.route(model).refuse(p.name, model, needs.capabilities())
}

// Needs is what a request asks of the model it is sent to.
type Needs struct {
	Images      bool
	Box         bool
	LongContext bool
}

// Check refuses what SystemOne refuses before it sends req with opts, and sends nothing. It
// validates req, then refuses what Encode refuses. A blank model is the built in default.
func (p Provider) Check(req Request, opts ...RequestOption) error {
	if err := p.check(); err != nil {
		return err
	}

	cfg, err := applyOptions(requestConfig{header: http.Header{}}, opts)
	if err != nil {
		return err
	}

	_, err = p.validated(req, cfg.longContext)

	return err
}

func (p Provider) validated(req Request, longContext bool) (encoded, error) {
	if err := ValidateRequest(req); err != nil {
		return encoded{}, err
	}

	return encodeRequest(p, req, longContext)
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
	return req.Needs().capabilities()
}

// Needs is what req asks of the model, the value Provider.Refuse takes: images for assets, a box for
// a box question, and long context when LongContext is set.
func (req Request) Needs() Needs {
	return Needs{
		Images:      len(req.Assets) > 0,
		Box:         asksBox(req.Questions),
		LongContext: req.LongContext,
	}
}

func asksBox(questions Questions) bool {
	return slices.ContainsFunc(questions, func(named NamedQuestion) bool {
		_, isBox := named.Question.(Box)

		return isBox
	})
}

func (n Needs) capabilities() capabilities {
	return capabilities{images: n.Images, box: n.Box, longContext: n.LongContext}
}

func scanMembers(members []member) bodyScan {
	var scan bodyScan

	for _, m := range members {
		switch m.name {
		case "model":
			scan.model = modelName(m.value)
		case "assets":
			scan.assets = scan.assets || nonEmptyAssets(m.value)
		}
	}

	return scan
}

type bodyScan struct {
	model  string
	assets bool
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
