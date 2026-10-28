package onesie

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
)

// UnmarshalJSON reads the wire format in order, the last of a repeated id winning at its position. Null
// leaves q as it is. An object or array inside a question stays a json.RawMessage, keeping its key order.
func (q *Questions) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}

	entries, err := readMembers(bytes.Clone(data))
	if err != nil {
		return &ValidationError{Message: "questions must be an object"}
	}

	read := make(Questions, 0, len(entries))

	for _, entry := range lastOf(entries) {
		question, err := readQuestion(entry.name, entry.value)
		if err != nil {
			return err
		}

		read = append(read, NamedQuestion{ID: entry.name, Question: question})
	}

	*q = read

	return nil
}

func readQuestion(id string, value jsontext.Value) (Question, error) {
	members, err := readMembers(value)
	if err != nil {
		return nil, questionFault(id, "question '%s' is not an object")
	}

	fields := make(map[string]jsontext.Value, len(members))
	for _, m := range members {
		fields[m.name] = m.value
	}

	kind, err := questionType(id, fields["type"])
	if err != nil {
		return nil, err
	}

	instructions, err := wireValue(id, fields["instructions"])
	if err != nil {
		return nil, err
	}

	switch kind {
	case "noul":
		rubric, err := readRubric(id, fields["criteria"])

		return Noul{Instructions: instructions, Criteria: rubric}, err
	case "choice":
		options, err := readOptions(id, fields["criteria"])

		return Choice{Instructions: instructions, Criteria: options}, err
	case "score":
		levels, err := readLevels(id, fields["criteria"])

		return Score{Instructions: instructions, Criteria: levels}, err
	case "box":
		return Box{Instructions: instructions}, nil
	default:
		return nil, questionFault(id, "question '%s' has the type '%s', which is not "+answerKinds, printable(kind))
	}
}

func questionType(id string, value jsontext.Value) (string, error) {
	if value == nil {
		return "", questionFault(id, "question '%s' has no type")
	}

	var kind string
	if value.Kind() != '"' || json.Unmarshal(value, &kind) != nil {
		return "", questionFault(id, "question '%s' has a type that is not a string")
	}

	return kind, nil
}

func readRubric(id string, value jsontext.Value) (*NoulCriteria, error) {
	if value == nil || value.Kind() == 'n' {
		return nil, nil
	}

	members, err := readMembers(value)
	if err != nil {
		return nil, questionFault(id, "noul question '%s' needs criteria as an object, or none")
	}

	var rubric NoulCriteria

	for _, m := range members {
		switch m.name {
		case "true":
			rubric.True, err = wireValue(id, m.value)
		case "false":
			rubric.False, err = wireValue(id, m.value)
		}

		if err != nil {
			return nil, err
		}
	}

	return &rubric, nil
}

func readOptions(id string, value jsontext.Value) (Criteria, error) {
	members, err := readMembers(value)
	if err != nil {
		return nil, questionFault(id, "choice question '%s' needs criteria as an object")
	}

	options := make(Criteria, 0, len(members))

	for _, m := range lastOf(members) {
		desc, err := wireValue(id, m.value)
		if err != nil {
			return nil, err
		}

		options = append(options, NamedCriterion{Name: m.name, Desc: desc})
	}

	return options, nil
}

func readLevels(id string, value jsontext.Value) ([]any, error) {
	var raw []json.RawMessage
	if value.Kind() != '[' || json.Unmarshal(value, &raw) != nil {
		return nil, questionFault(id, "score question '%s' needs criteria as an array")
	}

	levels := make([]any, 0, len(raw))

	for _, level := range raw {
		read, err := wireValue(id, level)
		if err != nil {
			return nil, err
		}

		levels = append(levels, read)
	}

	return levels, nil
}

func wireValue(id string, value jsontext.Value) (any, error) {
	switch value.Kind() {
	case 0:
		return nil, nil
	case '{', '[':
		return value, nil
	}

	var scalar any
	if err := json.Unmarshal(value, &scalar); err != nil {
		return nil, questionFault(id, "question '%s' has a value that cannot be read: %v", err)
	}

	return scalar, nil
}

func questionFault(id, format string, args ...any) *ValidationError {
	return &ValidationError{Question: id, Message: fmt.Sprintf(format, append([]any{printable(id)}, args...)...)}
}
