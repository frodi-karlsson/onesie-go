package onesie

import (
	"bytes"
	"encoding/json/jsontext"
)

func takeLongContext(body []byte) ([]byte, bool, error) {
	if !bytes.Contains(body, []byte(`"long_context"`)) {
		return body, false, nil
	}

	members, readable := objectMembers(body)
	if !readable {
		// Forwarded as it is, so the server reports a body it cannot read, as it does today.
		return body, false, nil
	}

	var (
		kept       []member
		found, set bool
	)

	for _, m := range members {
		if m.name != "long_context" {
			kept = append(kept, m)

			continue
		}

		found = true

		switch string(m.value) {
		case "true":
			set = true
		case "false":
		default:
			return nil, false, &ValidationError{Message: "long_context must be true or false"}
		}
	}

	if !found {
		return body, false, nil
	}

	rewritten, err := writeMembers(kept)
	if err != nil {
		return nil, false, err
	}

	return rewritten, set, nil
}

func objectMembers(body []byte) ([]member, bool) {
	if jsontext.NewDecoder(bytes.NewReader(body), rawOptions...).PeekKind() != '{' {
		return nil, false
	}

	members, err := readMembers(body)

	return members, err == nil
}

func writeMembers(members []member) ([]byte, error) {
	var out bytes.Buffer

	enc := jsontext.NewEncoder(&out, rawOptions...)
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return nil, err
	}

	for _, m := range members {
		if err := enc.WriteToken(jsontext.String(m.name)); err != nil {
			return nil, err
		}

		if err := enc.WriteValue(m.value); err != nil {
			return nil, err
		}
	}

	if err := enc.WriteToken(jsontext.EndObject); err != nil {
		return nil, err
	}

	// The encoder ends a top level value with a newline, which a request body does not need.
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}
