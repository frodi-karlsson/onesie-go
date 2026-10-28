package onesie

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
)

// The question limits the API documents. ValidateQuestions checks them, and SystemOneRaw checks a raw body.
const (
	// MaxChoiceOptions is the most options a choice question may have.
	MaxChoiceOptions = 255
	// MaxScoreLevels is the most levels a score question may have.
	MaxScoreLevels = 10
)

// Levels widens plain strings into Score criteria, which is what most rubrics are.
func Levels(levels ...string) []any {
	widened := make([]any, len(levels))
	for i, level := range levels {
		widened[i] = level
	}

	return widened
}

// ValidateQuestions rejects a request the API would reject, before it is sent.
func ValidateQuestions(questions Questions) error {
	if len(questions) == 0 {
		return &ValidationError{Message: "at least one question is required"}
	}

	seen := make(map[string]struct{}, len(questions))

	for _, named := range questions {
		// The body is keyed by id, so a repeated one collapses into a single API answer.
		if _, taken := seen[named.ID]; taken {
			return &ValidationError{
				Question: named.ID,
				Message:  fmt.Sprintf("duplicate question id '%s'", printable(named.ID)),
			}
		}

		seen[named.ID] = struct{}{}

		if isNil(named.Question) {
			return &ValidationError{
				Question: named.ID,
				Message:  fmt.Sprintf("question '%s' must not be nil", printable(named.ID)),
			}
		}

		if err := named.Question.validate(named.ID); err != nil {
			return err
		}
	}

	return nil
}

// Questions is an ordered list written to the wire as a JSON object in slice order. A shared value
// must not be appended to, since two appends can write the same backing array.
type Questions []NamedQuestion

// NamedQuestion pairs a question with the id it answers under.
type NamedQuestion struct {
	ID       string
	Question Question
}

// MarshalJSON writes the questions as a JSON object, keeping slice order.
func (q Questions) MarshalJSON() ([]byte, error) {
	pairs := make([]pair, 0, len(q))
	for _, named := range q {
		pairs = append(pairs, pair{key: named.ID, value: named.Question})
	}

	return marshalObject(pairs)
}

func isNil(question Question) bool {
	if question == nil {
		return true
	}

	// A typed nil pointer is not equal to nil, and every validate has a value receiver, so calling
	// through one would dereference it and panic.
	value := reflect.ValueOf(question)

	return value.Kind() == reflect.Pointer && value.IsNil()
}

// Question is one of Noul, Choice, Score or Box. The interface is closed, because validate is
// unexported, so no type outside this package can satisfy it.
type Question interface {
	json.Marshaler

	validate(name string) error
}

// Noul asks a yes or no question and is answered with the probability of yes.
type Noul struct {
	Instructions any
	Criteria     *NoulCriteria
}

// MarshalJSON encodes the question in the shape the API expects.
func (q Noul) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type         string        `json:"type"`
		Instructions any           `json:"instructions"`
		Criteria     *NoulCriteria `json:"criteria,omitempty"`
	}{Type: "noul", Instructions: q.Instructions, Criteria: q.Criteria})
}

func (Noul) validate(string) error {
	return nil
}

// NoulCriteria describes what a yes and a no mean.
type NoulCriteria struct {
	True  any `json:"true"`
	False any `json:"false"`
}

// Choice picks one option from a set and is answered with a distribution over them.
type Choice struct {
	Instructions any
	Criteria     Criteria
}

// MarshalJSON encodes the question in the shape the API expects.
func (q Choice) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type         string   `json:"type"`
		Instructions any      `json:"instructions"`
		Criteria     Criteria `json:"criteria"`
	}{Type: "choice", Instructions: q.Instructions, Criteria: q.Criteria})
}

func (q Choice) validate(name string) error {
	if count := distinctNames(q.Criteria); count > MaxChoiceOptions {
		return tooManyOptions(name, count)
	}

	return nil
}

func distinctNames(criteria Criteria) int {
	// The criteria are written as a JSON object, so a repeated name is one option on the wire.
	names := make(map[string]struct{}, len(criteria))
	for _, named := range criteria {
		names[named.Name] = struct{}{}
	}

	return len(names)
}

// Criteria is an ordered set of choice options, kept as a slice so a body keeps its written order.
// Like Questions, a shared value must not be appended to.
type Criteria []NamedCriterion

// NamedCriterion is one option and what it means.
type NamedCriterion struct {
	Name string
	Desc any
}

// MarshalJSON writes the set as a JSON object in slice order. A nil set is null, so the zero value
// encodes to something the API rejects cleanly.
func (c Criteria) MarshalJSON() ([]byte, error) {
	if c == nil {
		return []byte("null"), nil
	}

	pairs := make([]pair, 0, len(c))
	for _, named := range c {
		pairs = append(pairs, pair{key: named.Name, value: named.Desc})
	}

	return marshalObject(pairs)
}

// Score rates the state against an ordered rubric and is answered with a weighted value.
type Score struct {
	Instructions any
	Criteria     []any
}

// MarshalJSON encodes the question in the shape the API expects.
func (q Score) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type         string `json:"type"`
		Instructions any    `json:"instructions"`
		Criteria     []any  `json:"criteria"`
	}{Type: "score", Instructions: q.Instructions, Criteria: q.Criteria})
}

func (q Score) validate(name string) error {
	switch {
	case len(q.Criteria) < 2:
		return &ValidationError{
			Question: name,
			Message:  fmt.Sprintf("score question '%s' has %d criteria, at least two are required", printable(name), len(q.Criteria)),
		}
	case len(q.Criteria) > MaxScoreLevels:
		return tooManyLevels(name, len(q.Criteria))
	default:
		return nil
	}
}

func tooManyOptions(name string, count int) error {
	return &ValidationError{
		Question: name,
		Message:  fmt.Sprintf("choice question '%s' has %d options, and at most %d are allowed", printable(name), count, MaxChoiceOptions),
	}
}

func tooManyLevels(name string, count int) error {
	return &ValidationError{
		Question: name,
		Message:  fmt.Sprintf("score question '%s' has %d levels, and at most %d are allowed", printable(name), count, MaxScoreLevels),
	}
}

// Box asks where something is in an image and is answered with the edges of a box around it. It
// needs an image in the request and a model that serves it.
type Box struct {
	Instructions any
}

// MarshalJSON encodes the question in the shape the API expects.
func (q Box) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type         string `json:"type"`
		Instructions any    `json:"instructions"`
	}{Type: "box", Instructions: q.Instructions})
}

func (Box) validate(string) error {
	return nil
}

func marshalObject(pairs []pair) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteByte('{')

	for i, p := range pairs {
		if i > 0 {
			buf.WriteByte(',')
		}

		// Through json.Marshal rather than quoting by hand, because a key is not character checked
		// here and one carrying a quote would otherwise produce a body no parser can read.
		key, err := json.Marshal(p.key)
		if err != nil {
			return nil, err
		}

		buf.Write(key)
		buf.WriteByte(':')

		value, err := json.Marshal(p.value)
		if err != nil {
			return nil, err
		}

		buf.Write(value)
	}

	buf.WriteByte('}')

	return buf.Bytes(), nil
}

type pair struct {
	key   string
	value any
}
