package onesie

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// NewAPIError builds the APIError a live response with this status and body gets from p's route for
// model, sentinels included, for a fake server or transport. A blank model is p's built in default.
func NewAPIError(p Provider, model string, needs Needs, status int, body []byte) *APIError {
	var via route
	if p.check() == nil {
		via = p.route(modelOr(model, p.defaultModel))
	}

	return newAPIError(via, needs, status, http.Header{}, body, time.Time{})
}

func newAPIError(via route, needs Needs, status int, header http.Header, body []byte, now time.Time) *APIError {
	parsed := decodeBody(body)
	retryAfter, _ := parseRetryAfter(header, now)
	described := describe(status, header, parsed, body, via.read)

	return &APIError{
		Status:     status,
		RequestID:  header.Get(via.requestIDHeader),
		Header:     header,
		Body:       parsed,
		RawBody:    body,
		RetryAfter: retryAfter,
		Code:       codeOf(parsed),
		message:    described.fitted(),
		detail:     described.said(),
		sentinel:   via.classify(status, parsed, described.whole(), needs),
	}
}

func oversizedAPIError(via route, needs Needs, status int, header http.Header, limit int64, now time.Time) *APIError {
	retryAfter, _ := parseRetryAfter(header, now)

	lead := statusLine(status)
	detail := capitalised(overLimit(limit))

	if redirected(status, header) {
		explanation := explainRedirect(header.Get("Location"))
		lead = fmt.Sprintf("%d %s", status, explanation)
		detail = capitalised(explanation) + ", and " + overLimit(limit)
	}

	return &APIError{
		Status:     status,
		RequestID:  header.Get(via.requestIDHeader),
		Header:     header,
		RetryAfter: retryAfter,
		message:    lead + ", and " + overLimit(limit),
		detail:     detail,
		// Classified on the status alone, since the note is onesie's own text and no text rule may match it.
		sentinel: via.classify(status, nil, "", needs),
	}
}

func overLimit(limit int64) string {
	const mebibyte = 1 << 20

	if limit%mebibyte == 0 {
		return fmt.Sprintf("the response body is over the %d MiB limit", limit/mebibyte)
	}

	return fmt.Sprintf("the response body is over the %d byte limit", limit)
}

func describe(status int, header http.Header, body any, raw []byte, read func(any) string) description {
	switch {
	case redirected(status, header):
		explanation := explainRedirect(header.Get("Location"))
		return description{lead: fmt.Sprintf("%d %s", status, explanation), explanation: capitalised(explanation)}
	case body == nil && len(raw) == 0:
		return description{lead: fmt.Sprintf("%d status code, no body", status)}
	case body == nil:
		return describeText(status, string(raw))
	}

	if text, isText := body.(string); isText {
		return describeString(status, text)
	}

	if detail := read(body); firstLine(detail) != "" {
		return description{lead: fmt.Sprintf("%d ", status), detail: detail}
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return description{lead: fmt.Sprintf("%d status code, unreadable body", status)}
	}

	return description{lead: fmt.Sprintf("%d ", status), detail: string(encoded), encoded: true}
}

type description struct {
	lead        string
	detail      string
	encoded     bool
	explanation string
}

func (d description) whole() string {
	return d.lead + d.detail
}

func (d description) fitted() string {
	// Only the detail is cut, since it comes from the body, so a hostile body cannot reach a log line
	// at full length or with its own control characters. Classifying still reads the whole detail.
	return d.lead + truncate(firstLine(d.detail))
}

func (d description) said() string {
	switch {
	case d.explanation != "":
		return d.explanation
	// An encoded body is shown so a reader sees something, but the response said nothing readable.
	case d.encoded:
		return ""
	}

	return truncate(firstLine(d.detail))
}

func redirected(status int, header http.Header) bool {
	return status >= 300 && status < 400 && header.Get("Location") != ""
}

func explainRedirect(location string) string {
	const refusal = ", which onesie does not follow, since the request carries the API key. " +
		"Point the base URL at where the API is"

	// Only the origin, since the rest of a location may carry a token.
	parsed, err := url.Parse(location)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "redirect" + refusal
	}

	return "redirect to " + printable(shownOf(parsed)) + refusal
}

func describeText(status int, body string) description {
	message := statusLine(status) + ", with a response body that is not JSON"

	// An HTML page from a proxy or a web server says nothing one line can show, while a line of text
	// may name the cause.
	line := firstLine(body)
	if line == "" || strings.HasPrefix(line, "<") || repeatsStatus(line, status) {
		return description{lead: message}
	}

	return description{lead: message + ": ", detail: line}
}

func describeString(status int, text string) description {
	line := firstLine(text)
	if line == "" || strings.HasPrefix(line, "<") {
		return description{lead: statusLine(status)}
	}

	return description{lead: fmt.Sprintf("%d ", status), detail: line}
}

func statusLine(status int) string {
	if http.StatusText(status) == "" {
		return fmt.Sprintf("%d status code", status)
	}

	return fmt.Sprintf("%d %s", status, http.StatusText(status))
}

func repeatsStatus(line string, status int) bool {
	text := strings.TrimSuffix(line, ".")
	text = strings.TrimSpace(strings.TrimPrefix(text, strconv.Itoa(status)))

	return strings.EqualFold(text, http.StatusText(status))
}

func readMessage(body any) string {
	if text, ok := body.(string); ok {
		return text
	}

	fields, ok := body.(map[string]any)
	if !ok {
		return ""
	}

	if text, ok := fields["error"].(string); ok {
		return text
	}

	if nested, ok := fields["error"].(map[string]any); ok {
		if text, ok := nested["message"].(string); ok {
			return text
		}
	}

	if text, ok := fields["message"].(string); ok {
		return text
	}

	if text, ok := fields["detail"].(string); ok {
		return text
	}

	if nested, ok := fields["detail"].(map[string]any); ok {
		if text, ok := nested["message"].(string); ok {
			return text
		}

		if text, ok := nested["error_type"].(string); ok {
			return text
		}
	}

	if list, ok := fields["detail"].([]any); ok {
		return describeValidation(list)
	}

	return firstError(fields)
}

func firstError(fields map[string]any) string {
	list, isList := fields["errors"].([]any)
	if !isList || len(list) == 0 {
		return ""
	}

	entry, isObject := list[0].(map[string]any)
	if !isObject {
		return ""
	}

	text, isText := entry["message"].(string)
	if !isText {
		return ""
	}

	return text
}

func codeOf(body any) string {
	fields, isObject := body.(map[string]any)
	if !isObject {
		return ""
	}

	if nested, isNested := fields["error"].(map[string]any); isNested {
		return codeText(nested["code"])
	}

	if list, isList := fields["errors"].([]any); isList && len(list) > 0 {
		if entry, isEntry := list[0].(map[string]any); isEntry {
			return codeText(entry["code"])
		}
	}

	return ""
}

func codeText(code any) string {
	switch typed := code.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func describeValidation(entries []any) string {
	parts := make([]string, 0, len(entries))

	for _, entry := range entries {
		fields, ok := entry.(map[string]any)
		if !ok {
			continue
		}

		msg, ok := fields["msg"].(string)
		if !ok {
			continue
		}

		path := validationPath(fields["loc"])
		if path == "" {
			parts = append(parts, msg)

			continue
		}

		parts = append(parts, path+": "+msg)
	}

	return strings.Join(parts, ", ")
}

func validationPath(loc any) string {
	list, ok := loc.([]any)
	if !ok {
		return ""
	}

	segments := make([]string, 0, len(list))

	// The location comes from FastAPI, which prefixes it with the request part.
	for _, item := range list {
		text := fmt.Sprint(item)
		if text == "body" {
			continue
		}

		segments = append(segments, text)
	}

	return strings.Join(segments, ".")
}

func decodeBody(body []byte) any {
	var parsed any
	// Lenient, since proxies do not always set a JSON content type.
	if json.Unmarshal(body, &parsed) != nil {
		return nil
	}

	return parsed
}
