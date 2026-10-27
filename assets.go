package onesie

import (
	"fmt"
	"strings"
)

func unreferenced(texts, refs []string) []string {
	matcher := assetMatcher{refs: refs}
	used := make(map[string]bool, len(refs))

	for _, text := range texts {
		for _, ref := range matcher.refsIn(text) {
			used[ref] = true
		}
	}

	var missing []string

	for _, ref := range refs {
		if !used[ref] {
			missing = append(missing, ref)
		}
	}

	return missing
}

type assetMatcher struct {
	refs []string
}

func (m assetMatcher) refsIn(text string) []string {
	var found []string

	for i := 0; i < len(text); {
		ref := m.longestAt(text[i:])
		if ref == "" {
			i++

			continue
		}

		found = append(found, ref)
		i += len(ref)
	}

	return found
}

func (m assetMatcher) longestAt(text string) string {
	longest := ""

	for _, ref := range m.refs {
		if len(ref) > len(longest) && strings.HasPrefix(text, ref) {
			longest = ref
		}
	}

	return longest
}

func notReferenced(ref string) *ValidationError {
	return &ValidationError{Message: fmt.Sprintf(
		"asset '%s' is not referenced in the state or the questions, and the API refuses that", ref)}
}

func checkFormat(ref, format string) *ValidationError {
	if wire := wireFormat(format); wire == "png" || wire == "jpg" {
		return nil
	}

	return &ValidationError{Message: fmt.Sprintf("image '%s' has format '%s', and an image must be png or jpg", ref, format)}
}
