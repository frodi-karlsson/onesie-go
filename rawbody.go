package onesie

import (
	"encoding/json"
	"slices"
	"strings"
)

// PrepareRawBody returns body as SystemOneRaw sends it before provider encoding, with no client. An absent,
// null or blank last model key becomes defaultModel, and longContext sets long_context to true.
func PrepareRawBody(body json.RawMessage, defaultModel string, longContext bool) json.RawMessage {
	if !json.Valid(body) {
		return body
	}

	members, readable := objectMembers(body)
	if !readable {
		return body
	}

	changed := false

	if defaultModel != "" && blankModel(members) {
		name, err := json.Marshal(defaultModel)
		if err != nil {
			return body
		}

		members, changed = replaceMembers(members, member{name: "model", value: name}, true), true
	}

	if longContext && !soleLongContext(members) {
		members, changed = replaceMembers(members, member{name: "long_context", value: []byte("true")}, false), true
	}

	if !changed {
		return body
	}

	rewritten, err := writeMembers(members)
	if err != nil {
		return body
	}

	return rewritten
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
	at := slices.IndexFunc(members, func(m member) bool { return m.name == replacement.name })
	kept := slices.DeleteFunc(slices.Clone(members), func(m member) bool { return m.name == replacement.name })

	switch {
	case at >= 0:
		return slices.Insert(kept, at, replacement)
	case first:
		return slices.Insert(kept, 0, replacement)
	default:
		return append(kept, replacement)
	}
}
