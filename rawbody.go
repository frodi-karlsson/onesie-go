package onesie

import (
	"encoding/json"
	"encoding/json/jsontext"
	"slices"
	"strings"
)

// PrepareRawBody fills an absent, null or blank last model key with defaultModel, as SystemOneRaw does first.
// longContext sets long_context true for a printed body fed back in, which SystemOneRaw sends as a header.
func PrepareRawBody(body json.RawMessage, defaultModel string, longContext bool) json.RawMessage {
	prepared := fillModel(parseBody(body), defaultModel)
	if longContext {
		prepared = setLongContext(prepared)
	}

	out, err := prepared.bytes()
	if err != nil {
		return body
	}

	return out
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
