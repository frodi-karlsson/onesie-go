package onesie

import (
	"net/http"
	"slices"
	"strings"
)

func classifyBy(rules ...errorRule) func(int, any, string, Needs) error {
	return func(status int, body any, message string, needs Needs) error {
		for _, rule := range rules {
			if sentinel := rule(status, body, message, needs); sentinel != nil {
				return sentinel
			}
		}

		return nil
	}
}

type errorRule func(status int, body any, message string, needs Needs) error

func cloudflareAuth(_ int, body any, _ string, _ Needs) error {
	// Cloudflare refuses a token with 10000 on most endpoints, and with 9106 under a 400 on the
	// models search.
	if code := codeOf(body); code == "10000" || code == "9106" {
		return ErrAuthentication
	}

	return nil
}

func unknownModel(phrase string) errorRule {
	return func(status int, _ any, message string, _ Needs) error {
		served := status == http.StatusBadRequest || status == http.StatusNotFound ||
			status == http.StatusUnprocessableEntity
		if served && strings.Contains(message, phrase) {
			return ErrUnknownModel
		}

		return nil
	}
}

func contextTooLong(_ int, _ any, message string, _ Needs) error {
	if strings.Contains(message, "max_tokens_exceeded") {
		return ErrContextTooLong
	}

	return nil
}

func boxNotServed(status int, _ any, message string, needs Needs) error {
	// A model that does not serve box questions answers one with a bare Invalid request.
	if status == http.StatusBadRequest && needs.Box && strings.Contains(strings.ToLower(message), "invalid request") {
		return ErrNotServed
	}

	return nil
}

func questionLimit(status int, _ any, message string, _ Needs) error {
	if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
		return nil
	}

	// A heuristic, since the server owns the wording. It pairs a word about a bound with the thing
	// counted, rather than pinning the one sentence seen live.
	lower := strings.ToLower(message)
	if containsAny(lower, "too many", "too few", "at most", "at least") && containsAny(lower, "level", "option", "image", "pixel") {
		return ErrQuestionLimit
	}

	return nil
}

func countDetail(sent func(body any) any) errorRule {
	return func(status int, body any, _ string, _ Needs) error {
		if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
			return nil
		}

		fields, isObject := sent(body).(map[string]any)
		if !isObject {
			return nil
		}

		entries, isList := fields["detail"].([]any)
		if !isList || !slices.ContainsFunc(entries, countEntry) {
			return nil
		}

		return ErrQuestionLimit
	}
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

func asSent(body any) any {
	return body
}

func containsAny(text string, phrases ...string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}

	return false
}
