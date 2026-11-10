package onesie

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"slices"
)

var bodyOrder = []string{"state", "model", "questions", "long_context", "assets"}

const (
	wholeBody layout = iota
	questionsOnly
)

func bodyOf(req Request, model string, shape layout) (jsonBody, error) {
	body, err := writeBody(req, model, shape)
	if err != nil {
		return jsonBody{}, unencodable(err)
	}

	return body, nil
}

func writeBody(req Request, model string, shape layout) (jsonBody, error) {
	var out bytes.Buffer

	enc := jsontext.NewEncoder(&out, bodyOptions)
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return jsonBody{}, err
	}

	type span struct {
		name       string
		start, end int64
	}

	var spans []span

	for _, part := range shape.parts(req, model) {
		if err := enc.WriteToken(jsontext.String(part.name)); err != nil {
			return jsonBody{}, err
		}

		start := enc.OutputOffset()

		if err := jsonv2.MarshalEncode(enc, part.value, bodyOptions); err != nil {
			return jsonBody{}, err
		}

		spans = append(spans, span{name: part.name, start: start, end: enc.OutputOffset()})
	}

	if err := enc.WriteToken(jsontext.EndObject); err != nil {
		return jsonBody{}, err
	}

	raw := withoutNewline(out.Bytes())
	members := make([]member, 0, len(spans))

	for _, s := range spans {
		// The encoder writes the colon before a member's value, so the value starts one byte after its name.
		members = append(members, member{name: s.name, value: raw[s.start+1 : s.end]})
	}

	return jsonBody{members: members, object: true, raw: raw, read: true}, nil
}

type layout int

func (shape layout) parts(req Request, model string) []bodyPart {
	var parts []bodyPart

	for _, name := range bodyOrder {
		if value, kept := shape.member(name, req, model); kept {
			parts = append(parts, bodyPart{name: name, value: value})
		}
	}

	return parts
}

type bodyPart struct {
	name  string
	value any
}

func (shape layout) member(name string, req Request, model string) (any, bool) {
	switch name {
	case "state":
		return req.State, shape != questionsOnly
	case "model":
		return model, true
	case "questions":
		return req.Questions, true
	case "long_context":
		return true, req.LongContext
	default:
		return req.Assets, shape == wholeBody && len(req.Assets) > 0
	}
}

func inserted(members []member, added member) []member {
	rank := slices.Index(bodyOrder, added.name)

	at := slices.IndexFunc(members, func(m member) bool { return slices.Index(bodyOrder, m.name) > rank })
	if at < 0 {
		at = len(members)
	}

	return slices.Insert(slices.Clone(members), at, added)
}
