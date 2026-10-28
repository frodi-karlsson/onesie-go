package onesie

import (
	"net/http"
	"slices"
	"strings"
)

func classifyBy(rules ...errorRule) func(reply) error {
	return func(r reply) error {
		for _, rule := range rules {
			if sentinel := rule(r); sentinel != nil {
				return sentinel
			}
		}

		return nil
	}
}

type errorRule func(r reply) error

type reply struct {
	status  int
	body    any
	inner   any
	message string
	needs   Needs
}

func cloudflareAuth(r reply) error {
	// Cloudflare refuses a token with 10000 on most endpoints, and with 9106 under a 400 on the
	// models search.
	if code := codeOf(r.body); code == "10000" || code == "9106" {
		return ErrAuthentication
	}

	return nil
}

func unknownModel(phrase string) errorRule {
	return func(r reply) error {
		served := r.status == http.StatusBadRequest || r.status == http.StatusNotFound ||
			r.status == http.StatusUnprocessableEntity
		if served && strings.Contains(r.message, phrase) {
			return ErrUnknownModel
		}

		return nil
	}
}

func contextTooLong(r reply) error {
	if strings.Contains(r.message, "max_tokens_exceeded") {
		return ErrContextTooLong
	}

	return nil
}

func boxNotServed(r reply) error {
	// A model that does not serve box questions answers one with a bare Invalid request.
	if r.status == http.StatusBadRequest && r.needs.Box && strings.Contains(strings.ToLower(r.message), "invalid request") {
		return ErrNotServed
	}

	return nil
}

func questionLimit(r reply) error {
	if r.status != http.StatusBadRequest && r.status != http.StatusUnprocessableEntity {
		return nil
	}

	// A heuristic, since the server owns the wording. It pairs a word about a bound with the thing
	// counted, rather than pinning the one sentence seen live.
	lower := strings.ToLower(r.message)
	if containsAny(lower, "too many", "too few", "at most", "at least") && containsAny(lower, "level", "option", "image", "pixel") {
		return ErrQuestionLimit
	}

	return nil
}

func countDetail(r reply) error {
	if r.status != http.StatusBadRequest && r.status != http.StatusUnprocessableEntity {
		return nil
	}

	fields, isObject := r.inner.(map[string]any)
	if !isObject {
		return nil
	}

	entries, isList := fields["detail"].([]any)
	if !isList || !slices.ContainsFunc(entries, countEntry) {
		return nil
	}

	return ErrQuestionLimit
}

func countEntry(entry any) bool {
	fields, isObject := entry.(map[string]any)
	if !isObject {
		return false
	}

	kind := fields["type"]
	loc, isList := fields["loc"].([]any)

	return (kind == "too_long" || kind == "too_short") && isList && len(loc) >= 2 && loc[0] == "body" && loc[1] == "questions"
}

func containsAny(text string, phrases ...string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}

	return false
}
