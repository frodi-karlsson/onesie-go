package onesie

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"
)

// PrepareRaw prepares body as SystemOneRaw sends it, refusing what a send refuses. Repeated keys
// resolve to the last, a blank model to defaultModel, and WithLongContext to long_context.
func (p Provider) PrepareRaw(body json.RawMessage, defaultModel string, opts ...RequestOption) (*PreparedBody, error) {
	if err := p.check(); err != nil {
		return nil, err
	}

	cfg, err := applyOptions(requestConfig{header: http.Header{}}, opts)
	if err != nil {
		return nil, err
	}

	return p.prepare(bytes.Clone(body), defaultModel, cfg.longContext)
}

func (p Provider) prepare(raw []byte, defaultModel string, longContext bool) (*PreparedBody, error) {
	if _, err := compactBody(raw); err != nil {
		return nil, err
	}

	if !utf8.Valid(raw) {
		return nil, &ValidationError{Message: "the request body is not valid UTF-8"}
	}

	model := orDefault(strings.TrimSpace(defaultModel), p.defaultModel)
	b := fillModel(lastWins(parseBody(raw)), model)

	bodyLongContext, err := longContextOf(b.members)
	if err != nil {
		return nil, err
	}

	asked, err := questionsIn(b.members)
	if err != nil {
		return nil, err
	}

	if err = checkQuestionCounts(asked); err != nil {
		return nil, err
	}

	if longContext {
		b = setLongContext(b)
	}

	scan := scanMembers(b.members)
	model = orDefault(scan.model, model)
	via := p.route(model)
	needs := capabilities{images: scan.assets, box: asksBox(asked), longContext: longContext || bodyLongContext}

	if needs.images || needs.box {
		if fault := checkBodyImages(b.members); fault != nil {
			return nil, fault
		}
	}

	if refusal := via.refuse(p.name, model, needs); refusal != nil {
		return nil, refusal
	}

	sent, err := encodeBody(via, needs, withoutLongContext(b))
	if err != nil {
		return nil, err
	}

	shown, err := b.bytes()
	if err != nil {
		return nil, err
	}

	return &PreparedBody{provider: p.name, shown: shown, sent: sent, model: model, asked: asked}, nil
}

// PreparedBody is a raw body PrepareRaw checked and encoded for one provider, which SendPrepared
// sends as it is. Its facts are the ones that send uses.
type PreparedBody struct {
	provider string
	shown    []byte
	sent     encoded
	model    string
	asked    Questions
}

// Bytes returns a copy of the prepared body, as a print of the request shows it.
func (b *PreparedBody) Bytes() json.RawMessage {
	return bytes.Clone(b.shown)
}

// Model is the model the send names, the body's own or the default it was prepared with.
func (b *PreparedBody) Model() string {
	return b.model
}

// LongContext reports whether the send raises the context limits, from the body or WithLongContext.
func (b *PreparedBody) LongContext() bool {
	return b.sent.needs.longContext
}

// Box reports whether the body asks a box question.
func (b *PreparedBody) Box() bool {
	return b.sent.needs.box
}

// QuestionCount is the number of distinct ids in the body's questions member, each of which a send
// checks the answer to, or 0 when that member is absent or not an object.
func (b *PreparedBody) QuestionCount() int {
	return len(b.asked)
}

func questionsIn(members []member) (Questions, error) {
	at := slices.IndexFunc(members, named("questions"))
	if at < 0 || members[at].value.Kind() != '{' {
		return nil, nil
	}

	var asked Questions
	if err := asked.UnmarshalJSON(members[at].value); err != nil {
		return nil, err
	}

	return asked, nil
}

func lastWins(b jsonBody) jsonBody {
	if !b.object {
		return b
	}

	kept := lastOf(b.members)
	if len(kept) == len(b.members) {
		return b
	}

	return b.with(kept)
}

func lastOf(members []member) []member {
	last := make(map[string]int, len(members))
	for i, m := range members {
		last[m.name] = i
	}

	if len(last) == len(members) {
		return members
	}

	kept := make([]member, 0, len(last))

	for i, m := range members {
		if last[m.name] == i {
			kept = append(kept, m)
		}
	}

	return kept
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
