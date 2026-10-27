package onesie

import "slices"

func takeLongContext(b jsonBody) (jsonBody, bool, error) {
	if !b.object || !slices.ContainsFunc(b.members, named("long_context")) {
		return b, false, nil
	}

	on, err := longContextOf(b.members)
	if err != nil {
		return jsonBody{}, false, err
	}

	return b.with(slices.DeleteFunc(slices.Clone(b.members), named("long_context"))), on, nil
}

func longContextOf(members []member) (bool, error) {
	on := false

	for _, m := range members {
		if m.name != "long_context" {
			continue
		}

		switch string(m.value) {
		case "true":
			on = true
		case "false":
		default:
			return false, &ValidationError{Message: "long_context must be true or false"}
		}
	}

	return on, nil
}
