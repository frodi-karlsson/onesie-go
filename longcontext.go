package onesie

import "slices"

func withoutLongContext(b jsonBody) jsonBody {
	if !b.object || !slices.ContainsFunc(b.members, named("long_context")) {
		return b
	}

	return b.with(slices.DeleteFunc(slices.Clone(b.members), named("long_context")))
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
