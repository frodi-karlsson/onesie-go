package onesie

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxBodyInError = 200
	byteOrderMark  = "\ufeff"
)

// Sentinels for errors.Is. An APIError reports itself as the one matching its status.
var (
	// ErrBadRequest matches an APIError with status 400.
	ErrBadRequest = errors.New("bad request")
	// ErrAuthentication matches an APIError with status 401.
	ErrAuthentication = errors.New("authentication failed")
	// ErrPaymentRequired matches an APIError with status 402, which OpenRouter sends when the account
	// is out of credits.
	ErrPaymentRequired = errors.New("payment required")
	// ErrPermissionDenied matches an APIError with status 403.
	ErrPermissionDenied = errors.New("permission denied")
	// ErrNotFound matches an APIError with status 404.
	ErrNotFound = errors.New("not found")
	// ErrUnprocessableEntity matches an APIError with status 422.
	ErrUnprocessableEntity = errors.New("unprocessable entity")
	// ErrRateLimit matches an APIError with status 429.
	ErrRateLimit = errors.New("rate limited")
	// ErrServer matches an APIError with any status from 500 to 599.
	ErrServer = errors.New("server error")
	// ErrConnection matches a ConnectionError, including a TimeoutError.
	ErrConnection = errors.New("connection error")
	// ErrTimeout matches a TimeoutError.
	ErrTimeout = errors.New("timed out")
	// ErrResponse matches a ResponseError.
	ErrResponse = errors.New("unusable response")
	// ErrValidation matches a ValidationError.
	ErrValidation = errors.New("invalid request")
)

// NewAPIError builds an APIError from a status and a message alone, for a failure that no response
// carried, such as one a mock answers file names.
func NewAPIError(status int, message string) *APIError {
	return &APIError{Status: status, Header: http.Header{}, message: message}
}

func newAPIError(status int, header http.Header, requestIDHeader string, body []byte, now time.Time) *APIError {
	parsed := decodeBody(body)
	isJSON := len(body) > 0 && json.Valid(body)

	message := describe(status, header, parsed)
	if len(body) > 0 && !isJSON && !redirected(status, header) {
		message = describeText(status, string(body))
	}

	retryAfter, _ := parseRetryAfter(header, now)

	return &APIError{
		Status:     status,
		RequestID:  header.Get(requestIDHeader),
		Header:     header,
		Body:       parsed,
		RawBody:    body,
		BodyJSON:   isJSON,
		RetryAfter: retryAfter,
		message:    message,
		keyRefused: cloudflareAuthFailure(parsed),
	}
}

func redirected(status int, header http.Header) bool {
	return status >= 300 && status < 400 && header.Get("Location") != ""
}

func describeText(status int, body string) string {
	statusLine := fmt.Sprintf("%d %s", status, http.StatusText(status))
	if http.StatusText(status) == "" {
		statusLine = fmt.Sprintf("%d status code", status)
	}

	message := "onesie: " + statusLine + ", with a response body that is not JSON"

	// An HTML page from a proxy or a web server says nothing one line can show, while a line of text
	// may name the cause.
	line := firstLine(body)
	if line == "" || strings.HasPrefix(line, "<") || repeatsStatus(line, status) {
		return message
	}

	return message + ": " + line
}

func firstLine(body string) string {
	line, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(body), byteOrderMark), "\n")

	return printable(line)
}

func printable(text string) string {
	return truncate(strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}

		return r
	}, text)))
}

func repeatsStatus(line string, status int) bool {
	text := strings.TrimSuffix(line, ".")
	text = strings.TrimSpace(strings.TrimPrefix(text, strconv.Itoa(status)))

	return strings.EqualFold(text, http.StatusText(status))
}

func cloudflareAuthFailure(body any) bool {
	fields, isObject := body.(map[string]any)
	if !isObject {
		return false
	}

	list, isList := fields["errors"].([]any)
	if !isList || len(list) == 0 {
		return false
	}

	entry, isEntry := list[0].(map[string]any)
	if !isEntry {
		return false
	}

	// Cloudflare refuses a token with 10000 on most endpoints, and with 9106 under a 400 on the
	// models search.
	code, isNumber := entry["code"].(float64)

	return isNumber && (code == 10000 || code == 9106)
}

func describe(status int, header http.Header, body any) string {
	if redirected(status, header) {
		return fmt.Sprintf("onesie: %d redirect to %s, which onesie does not follow, since the request "+
			"carries the API key. Point the base URL at where the API is", status, truncate(header.Get("Location")))
	}

	if detail := extractMessage(body); detail != "" {
		// Truncated here rather than inside extraction, so a hostile body arriving as a bare string
		// cannot reach a log line at full length.
		return fmt.Sprintf("onesie: %d %s", status, truncate(detail))
	}

	if body == nil {
		return fmt.Sprintf("onesie: %d status code, no body", status)
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Sprintf("onesie: %d status code, unreadable body", status)
	}

	return fmt.Sprintf("onesie: %d %s", status, truncate(string(encoded)))
}

func truncate(raw string) string {
	if len(raw) <= maxBodyInError {
		return raw
	}

	cut := maxBodyInError
	for cut > 0 && !utf8.RuneStart(raw[cut]) {
		cut--
	}

	return raw[:cut] + "..."
}

func extractMessage(body any) string {
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
			return unwrapMessage(text)
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

	return trimWorkersAI(text)
}

func trimWorkersAI(text string) string {
	for {
		trimmed := strings.TrimPrefix(strings.TrimPrefix(text, "AiError: "), "Ai: ")
		if trimmed == text {
			break
		}

		text = trimmed
	}

	text = strings.TrimPrefix(text, "Model execution failed (User Input Error): ")

	// AI Gateway forwards TypeSafe's own error body as the rest of the message.
	if strings.HasPrefix(text, "{") && json.Valid([]byte(text)) {
		if inner := extractMessage(decodeBody([]byte(text))); inner != "" {
			return inner
		}
	}

	return text
}

func unwrapMessage(text string) string {
	// OpenRouter forwards a TypeSafe error as the text HTTP 400: followed by TypeSafe's body.
	if rest, ok := strings.CutPrefix(text, "HTTP "); ok && len(rest) > 5 && rest[3] == ':' {
		forwarded := []byte(strings.TrimSpace(rest[4:]))
		if json.Valid(forwarded) {
			if inner := extractMessage(decodeBody(forwarded)); inner != "" {
				return inner
			}
		}
	}

	// OpenRouter's validation puts its issue list in the message as a JSON string.
	var issues []struct {
		Path    any    `json:"path"`
		Message string `json:"message"`
	}

	if json.Unmarshal([]byte(text), &issues) != nil || len(issues) == 0 {
		return text
	}

	path := validationPath(issues[0].Path)
	if path == "" {
		return issues[0].Message
	}

	return path + ": " + issues[0].Message
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
	if len(body) == 0 {
		return nil
	}

	var parsed any
	// Lenient, since proxies do not always set a JSON content type.
	if err := json.Unmarshal(body, &parsed); err != nil {
		return string(body)
	}

	return parsed
}

// RetryAfterError reports a server requested delay onesie refused to wait out, because spending the
// remaining retries on a known answer is worse than failing now.
type RetryAfterError struct {
	APIError

	RetryAfter time.Duration
	Cap        time.Duration
}

// APIError is a non 2xx response from the API. Match it with errors.Is against a status sentinel,
// or recover it with errors.As to read the status, body and request ID.
type APIError struct {
	Status     int
	RequestID  string
	Header     http.Header
	Body       any
	RetryAfter time.Duration

	// RawBody is the response body as it arrived, and BodyJSON reports whether it parsed as JSON.
	// Body holds a body that did not parse as a string, as it holds a JSON string.
	RawBody  []byte
	BodyJSON bool

	message    string
	keyRefused bool
}

// Error returns the status and the message extracted from the response body. A body that is not JSON
// is summarised by its first line of text, and an HTML page is left out.
func (e *APIError) Error() string {
	return e.message
}

// Is reports whether this status matches one of the package status sentinels.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrBadRequest:
		return e.Status == http.StatusBadRequest
	case ErrAuthentication:
		return e.Status == http.StatusUnauthorized || e.keyRefused
	case ErrPaymentRequired:
		return e.Status == http.StatusPaymentRequired
	case ErrPermissionDenied:
		return e.Status == http.StatusForbidden
	case ErrNotFound:
		return e.Status == http.StatusNotFound
	case ErrUnprocessableEntity:
		return e.Status == http.StatusUnprocessableEntity
	case ErrRateLimit:
		return e.Status == http.StatusTooManyRequests
	case ErrServer:
		return e.Status >= 500 && e.Status <= 599
	default:
		return false
	}
}

// Error names the delay the server asked for and the cap that rejected it.
func (e *RetryAfterError) Error() string {
	return fmt.Sprintf(
		"onesie: status %d, the server asked to retry after %s, above the %s cap",
		e.Status, e.RetryAfter, e.Cap,
	)
}

// Unwrap returns the embedded APIError, so errors.As and errors.Is reach it.
func (e *RetryAfterError) Unwrap() error {
	return &e.APIError
}

// TimeoutError is a ConnectionError whose cause was the attempt deadline. It unwraps to
// context.DeadlineExceeded, so errors.Is against a caller's own deadline matches it too.
type TimeoutError struct {
	ConnectionError

	Timeout time.Duration
}

// ConnectionError is a transport failure, including a body that stopped arriving.
type ConnectionError struct {
	Err error
}

// Error describes the transport failure.
func (e *ConnectionError) Error() string {
	if e.Err == nil {
		return "onesie: connection error"
	}

	return "onesie: connection error: " + e.Err.Error()
}

// Unwrap returns the underlying transport error.
func (e *ConnectionError) Unwrap() error {
	return e.Err
}

// Is matches ErrConnection.
func (e *ConnectionError) Is(target error) bool {
	return target == ErrConnection
}

// Error reports the deadline that fired.
func (e *TimeoutError) Error() string {
	return fmt.Sprintf("onesie: request timed out after %s", e.Timeout)
}

// Unwrap returns the embedded ConnectionError, so errors.As reaches it.
func (e *TimeoutError) Unwrap() error {
	return &e.ConnectionError
}

// Is matches both ErrTimeout and ErrConnection.
func (e *TimeoutError) Is(target error) bool {
	return target == ErrConnection || target == ErrTimeout
}

// ResponseError is a 2xx response this client could not use, such as a body that is not the JSON
// the endpoint documents, or a response missing an answer that was asked for.
type ResponseError struct {
	Status int
	Body   []byte
	Err    error

	// Message replaces the generic sentence when the caller has a better one. The CLI's answer
	// checks do, since their wording is part of the published contract.
	Message string

	// Usage is what the response billed, when its body decoded that far, since the tokens were spent
	// even though the answers could not be used. Nil otherwise.
	Usage *Usage
}

// Error describes what could not be read.
func (e *ResponseError) Error() string {
	if e.Message != "" {
		return e.Message
	}

	return fmt.Sprintf("onesie: %d response could not be used: %v", e.Status, e.Err)
}

// Unwrap returns the underlying decoding error.
func (e *ResponseError) Unwrap() error {
	return e.Err
}

// Is matches ErrResponse.
func (e *ResponseError) Is(target error) bool {
	return target == ErrResponse
}

// ValidationError is a request this package rejected before sending it. Question names the
// offending question when one is to blame.
type ValidationError struct {
	Question string
	Message  string
}

// Error describes why the request was rejected.
func (e *ValidationError) Error() string {
	return "onesie: " + e.Message
}

// Is matches ErrValidation.
func (e *ValidationError) Is(target error) bool {
	return target == ErrValidation
}

func newCapabilityError(provider, model string, missing Needs, takers []string) *CapabilityError {
	var lacking []string

	if missing.Images {
		lacking = append(lacking, "images")
	}

	if missing.Box {
		lacking = append(lacking, "box questions")
	}

	if missing.LongContext {
		lacking = append(lacking, "long context")
	}

	message := fmt.Sprintf("the %s provider takes no %s", provider, joinNames(lacking, "or"))

	switch len(takers) {
	case 0:
	case 1:
		message += ". The " + takers[0] + " provider does"
	default:
		message += ". The " + joinNames(takers, "and") + " providers do"
	}

	return &CapabilityError{
		ValidationError: ValidationError{Message: message},
		Provider:        provider, Model: model, Missing: missing, Takers: takers,
	}
}

// CapabilityError is a request the provider's route for Model does not take. Missing holds only what
// it lacks, and Takers the other providers from Providers whose default model takes all of it.
type CapabilityError struct {
	ValidationError

	Provider string
	Model    string
	Missing  Needs
	Takers   []string
}

// Unwrap returns the embedded ValidationError, so errors.As reaches it.
func (e *CapabilityError) Unwrap() error {
	return &e.ValidationError
}

// AnswerError reports an answer that is absent or of another type.
type AnswerError struct {
	Name    string
	Want    string
	Got     string
	Missing bool
}

// Error describes whether the answer was absent or of the wrong type.
func (e *AnswerError) Error() string {
	if e.Missing {
		return fmt.Sprintf("onesie: no answer named %q", e.Name)
	}

	return fmt.Sprintf("onesie: answer %q is a %s, not a %s", e.Name, e.Got, e.Want)
}

// ResultShapeError is a response body that is not the System One result it should be. Answer names
// the answer at fault, or is empty when the body itself is wrong. Got is empty for a missing field.
type ResultShapeError struct {
	Answer string
	Field  string
	Want   string
	Got    string

	cause error
}

// Unwrap returns the decoder's error behind a wrong JSON type, or nil.
func (e *ResultShapeError) Unwrap() error {
	return e.cause
}

// Error describes the fault in the response's own terms, with no Go types. Text from the response is
// cleaned of control characters and cut short.
func (e *ResultShapeError) Error() string {
	field, got := printable(e.Field), printable(e.Got)

	if e.Answer == "" {
		return resultSentence(field, e.Want, got)
	}

	name := "answer '" + printable(e.Answer) + "'"
	if field == "" {
		return fmt.Sprintf("%s is %s, not %s", name, got, e.Want)
	}

	phrase, bare, verb := fieldPhrase(field)

	switch {
	case got == "":
		return fmt.Sprintf("%s has no %s, or it is null", name, bare)
	case field == "type" && e.Want == answerKinds:
		return fmt.Sprintf("%s has the type '%s', which is not %s", name, got, e.Want)
	case strings.HasPrefix(got, "an empty "):
		return fmt.Sprintf("%s has %s that %s empty", name, phrase, verb)
	default:
		return fmt.Sprintf("%s has %s that %s not %s", name, phrase, verb, e.Want)
	}
}

func resultSentence(field, want, got string) string {
	const lead = "the response is not a System One result"

	switch {
	case field == "" && got == "":
		return lead
	case field == "":
		return lead + ", since it is " + got
	case got == "":
		return lead + ", since it has no " + field + ", or it is null"
	default:
		_, _, verb := fieldPhrase(field)

		return lead + ", since its " + field + " " + verb + " not " + want
	}
}

func fieldPhrase(field string) (phrase, bare, verb string) {
	parent, key, nested := strings.Cut(field, ".")

	switch {
	case nested && parent == "probabilities":
		bare = "probability for '" + key + "'"
	case nested && parent == "legend":
		bare = "legend entry for '" + key + "'"
	case nested && parent == "box":
		bare = "box " + key + " edge"
	case !nested && strings.HasSuffix(field, "s"):
		return field, field, "are"
	default:
		bare = field
	}

	if strings.ContainsAny(bare[:1], "aeiou") {
		return "an " + bare, bare, "is"
	}

	return "a " + bare, bare, "is"
}
