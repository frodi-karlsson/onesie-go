package onesie

import (
	"encoding/json"
	"encoding/json/jsontext"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"
)

// PrepareRaw returns body as SystemOneRaw sees it before provider encoding, refusing what a send
// refuses. Repeated keys resolve to the last, a blank model to defaultModel, WithLongContext to long_context.
func (p Provider) PrepareRaw(body json.RawMessage, defaultModel string, opts ...RequestOption) (json.RawMessage, error) {
	if err := p.check(); err != nil {
		return nil, err
	}

	cfg, err := applyOptions(requestConfig{header: http.Header{}}, opts)
	if err != nil {
		return nil, err
	}

	prepared, err := p.prepare(body, defaultModel, cfg.longContext)
	if err != nil {
		return nil, err
	}

	if _, err = encodeBody(prepared.via, prepared.needs, withoutLongContext(prepared.body)); err != nil {
		return nil, err
	}

	return prepared.body.bytes()
}

func (p Provider) prepare(raw []byte, defaultModel string, longContext bool) (preparedBody, error) {
	if _, err := compactBody(raw); err != nil {
		return preparedBody{}, err
	}

	if !utf8.Valid(raw) {
		return preparedBody{}, &ValidationError{Message: "the request body is not valid UTF-8"}
	}

	model := orDefault(strings.TrimSpace(defaultModel), p.defaultModel)
	b := fillModel(lastWins(parseBody(raw)), model)

	bodyLongContext, err := longContextOf(b.members)
	if err != nil {
		return preparedBody{}, err
	}

	if err = checkQuestionCounts(b.members); err != nil {
		return preparedBody{}, err
	}

	if longContext {
		b = setLongContext(b)
	}

	scan := scanMembers(b.members)
	model = orDefault(scan.model, model)
	via := p.route(model)
	needs := capabilities{images: scan.assets, box: scan.box, longContext: longContext || bodyLongContext}

	if needs.images || needs.box {
		if fault := checkBodyImages(b.members); fault != nil {
			return preparedBody{}, fault
		}
	}

	if refusal := via.refuse(p.name, model, needs); refusal != nil {
		return preparedBody{}, refusal
	}

	return preparedBody{body: b, via: via, needs: needs}, nil
}

type preparedBody struct {
	body  jsonBody
	via   route
	needs capabilities
}

func lastWins(b jsonBody) jsonBody {
	if !b.object {
		return b
	}

	last := make(map[string]int, len(b.members))
	for i, m := range b.members {
		last[m.name] = i
	}

	if len(last) == len(b.members) {
		return b
	}

	kept := make([]member, 0, len(last))

	for i, m := range b.members {
		if last[m.name] == i {
			kept = append(kept, m)
		}
	}

	return b.with(kept)
}

func fillModel(b jsonBody, model string) jsonBody {
	if !b.object || model == "" || !blankModel(b.members) {
		return b
	}

	name, err := json.Marshal(model)
	if err != nil {
		return b
	}

	return b.with(replaceMembers(b.members, member{name: "model", value: name}, true))
}

func setLongContext(b jsonBody) jsonBody {
	if !b.object || soleLongContext(b.members) {
		return b
	}

	return b.with(replaceMembers(b.members, member{name: "long_context", value: jsontext.Value("true")}, false))
}

func blankModel(members []member) bool {
	var value []byte

	for _, m := range members {
		if m.name == "model" {
			value = m.value
		}
	}

	if value == nil || string(value) == "null" {
		return true
	}

	var model string

	return json.Unmarshal(value, &model) == nil && strings.TrimSpace(model) == ""
}

func soleLongContext(members []member) bool {
	found := 0

	for _, m := range members {
		if m.name != "long_context" {
			continue
		}

		if string(m.value) != "true" {
			return false
		}

		found++
	}

	return found == 1
}

func replaceMembers(members []member, replacement member, first bool) []member {
	at := slices.IndexFunc(members, named(replacement.name))
	kept := slices.DeleteFunc(slices.Clone(members), named(replacement.name))

	switch {
	case at >= 0:
		return slices.Insert(kept, at, replacement)
	case first:
		return slices.Insert(kept, 0, replacement)
	default:
		return append(kept, replacement)
	}
}
