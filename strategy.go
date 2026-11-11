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

	model = modelOr(model, p.defaultModel)

	return p.route(model).refuse(p.name, model, needs.capabilities())
}

// AlwaysLongContext reports whether model reads a state past the usual context limit without long
// context, so asking for it changes nothing there.
func (p Provider) AlwaysLongContext(model string) bool {
	if p.check() != nil {
		return false
	}

	return p.route(modelOr(model, p.defaultModel)).alwaysLongContext
}

// Needs is what a request asks of the model it is sent to.
type Needs struct {
	Images      bool
	Box         bool
	LongContext bool
}

// Prepare validates and encodes req as SystemOne would, with no client and no key, for SendPrepared to
// send. A blank model is the built in default, not EnvDefaultModel, and of opts only WithLongContext applies.
func (p Provider) Prepare(req Request, opts ...RequestOption) (*PreparedBody, error) {
	if err := p.check(); err != nil {
		return nil, err
	}

	cfg, err := applyOptions(requestConfig{header: http.Header{}}, opts)
	if err != nil {
		return nil, err
	}

	return p.prepare(req, cfg.longContext)
}

func (p Provider) prepare(req Request, longContext bool) (*PreparedBody, error) {
	req.Model = modelOr(req.Model, p.defaultModel)

	sent, body, err := p.validated(req, longContext)
	if err != nil {
		return nil, err
	}

	if sent.needs.longContext {
		body = setLongContext(body)
	}

	shown, err := body.bytes()
	if err != nil {
		return nil, err
	}

	return &PreparedBody{provider: p.name, shown: shown, sent: sent, model: req.Model, asked: detached(req.Questions)}, nil
}

func detached(questions Questions) Questions {
	kept := make(Questions, len(questions))
	for i, named := range questions {
		kept[i] = NamedQuestion{ID: named.ID, Question: detachedQuestion(dereferenced(named.Question))}
	}

	return kept
}

func detachedQuestion(asked Question) Question {
	// A check reads a choice's option names, which the caller's slice still holds, and of a score only
	// its level count, which the dereferenced copy already fixed.
	if choice, isChoice := asked.(Choice); isChoice {
		choice.Criteria = slices.Clone(choice.Criteria)
		return choice
	}

	return asked
}

func (p Provider) validated(req Request, longContext bool) (encoded, jsonBody, error) {
	if err := checkRequest(req); err != nil {
		return encoded{}, jsonBody{}, err
	}

	plan := p.plan(req, longContext)

	body, err := checkedBody(req, plan.model)
	if err != nil {
		return encoded{}, jsonBody{}, err
	}

	if err = plan.via.refuse(p.name, plan.model, plan.needs); err != nil {
		return encoded{}, jsonBody{}, err
	}

	sent, err := encodeBody(plan.via, plan.needs, body)

	return sent, body, err
}

func (p Provider) plan(req Request, longContext bool) requestPlan {
	model := modelOr(req.Model, p.defaultModel)
	needs := requestNeeds(req)
	needs.longContext = needs.longContext || longContext

	return requestPlan{model: model, via: p.route(model), needs: needs}
}

type requestPlan struct {
	model string
	via   route
	needs capabilities
}

type encoded struct {
	via    route
	body   []byte
	needs  capabilities
	images bool
}

func (e encoded) post(out any) call {
	return call{
		method: http.MethodPost, path: e.via.path, payload: e.body, out: out, decode: e.via.decode,
		via: e.via, needs: e.needs.asNeeds(), imagesHeader: e.images,
	}
}

func encodeBody(via route, needs capabilities, b jsonBody) (encoded, error) {
	withImages := needs.images || needs.box

	if withImages && via.images != nil {
		urls, err := b.dataURLs()
		if err != nil {
			return encoded{}, err
		}

		members, err := via.images(b.members, urls)
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
		_, isBox := dereferenced(named.Question).(Box)

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
