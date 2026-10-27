package onesie

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxBodyInError = 200
	byteOrderMark  = "\ufeff"
	answerKinds    = "noul, choice, score or box"
)

const (
	faultWrongKind shapeFault = iota
	faultMissing
	faultEmpty
	faultUnknownType
	faultNoAnswer
)

const (
	kindNothing jsonKind = iota
	kindObject
	kindArray
	kindString
	kindNumber
	kindBoolean
	kindNull
)

// Sentinels for errors.Is. An APIError matches the one for its status, and one its route reads from the body.
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
	// ErrUnknownModel matches an APIError for a model the provider does not serve.
	ErrUnknownModel = errors.New("unknown model")
	// ErrContextTooLong matches an APIError for a request longer than the model's context.
	ErrContextTooLong = errors.New("context too long")
	// ErrNotServed matches a request the model does not serve, a CapabilityError or an APIError alike.
	ErrNotServed = errors.New("not served")
	// ErrQuestionLimit matches an APIError refusing a question for a count of options, levels, images
	// or pixels. It is best effort, since the server owns the wording.
	ErrQuestionLimit = errors.New("question limit")
	// ErrConnection matches a ConnectionError, including a TimeoutError.
	ErrConnection = errors.New("connection error")
	// ErrTimeout matches a TimeoutError.
	ErrTimeout = errors.New("timed out")
	// ErrResponse matches a ResponseError.
	ErrResponse = errors.New("unusable response")
	// ErrValidation matches a ValidationError.
	ErrValidation = errors.New("invalid request")
	// ErrNoAPIKey matches the ValidationError New returns when it finds no API key, or an empty one.
	ErrNoAPIKey = errors.New("no API key")
	// ErrNoAccountID matches the ValidationError New returns when a provider with accounts has none.
	ErrNoAccountID = errors.New("no account id")
	// ErrInsecureBaseURL matches the BaseURLError New returns for plain http to a host other than
	// this machine.
	ErrInsecureBaseURL = errors.New("insecure base URL")
)

// NewAPIError builds an APIError from a status and a message, for fakes and mocks. The message
// carries no prefix, since Error adds it.
func NewAPIError(status int, message string) *APIError {
	return &APIError{Status: status, Header: http.Header{}, message: message}
}

func newAPIError(via route, needs Needs, status int, header http.Header, body []byte, now time.Time) *APIError {
	parsed := decodeBody(body)
	retryAfter, _ := parseRetryAfter(header, now)
	read := via.reader()

	return &APIError{
		Status:     status,
		RequestID:  header.Get(via.requestIDHeader),
		Header:     header,
		Body:       parsed,
		RawBody:    body,
		RetryAfter: retryAfter,
		Code:       codeOf(parsed),
		message:    describe(status, header, parsed, body, read, truncate),
		sentinel:   via.classify(status, parsed, describe(status, header, parsed, body, read, whole), needs),
	}
}

func whole(text string) string {
	return text
}

func redirected(status int, header http.Header) bool {
	return status >= 300 && status < 400 && header.Get("Location") != ""
}

func describeText(status int, body string, fit func(string) string) string {
	message := statusLine(status) + ", with a response body that is not JSON"

	// An HTML page from a proxy or a web server says nothing one line can show, while a line of text
	// may name the cause.
	line := firstLine(body)
	if line == "" || strings.HasPrefix(line, "<") || repeatsStatus(line, status) {
		return message
	}

	return message + ": " + fit(line)
}

func describeString(status int, text string, fit func(string) string) string {
	line := firstLine(text)
	if line == "" || strings.HasPrefix(line, "<") {
		return statusLine(status)
	}

	return fmt.Sprintf("%d %s", status, fit(line))
}

func statusLine(status int) string {
	if http.StatusText(status) == "" {
		return fmt.Sprintf("%d status code", status)
	}

	return fmt.Sprintf("%d %s", status, http.StatusText(status))
}

func firstLine(body string) string {
	line, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(body), byteOrderMark), "\n")

	return withoutControls(line)
}

func printable(text string) string {
	return truncate(withoutControls(text))
}

func withoutControls(text string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}

		return r
	}, text))
}

func repeatsStatus(line string, status int) bool {
	text := strings.TrimSuffix(line, ".")
	text = strings.TrimSpace(strings.TrimPrefix(text, strconv.Itoa(status)))

	return strings.EqualFold(text, http.StatusText(status))
}

func describe(status int, header http.Header, body any, raw []byte, read func(any) string, fit func(string) string) string {
	switch {
	case redirected(status, header):
		return describeRedirect(status, header.Get("Location"))
	case body == nil && len(raw) == 0:
		return fmt.Sprintf("%d status code, no body", status)
	case body == nil:
		return describeText(status, string(raw), fit)
	}

	if text, isText := body.(string); isText {
		return describeString(status, text, fit)
	}

	if detail := read(body); detail != "" {
		// Fitted here rather than inside extraction, so a hostile body cannot reach a log line at
		// full length.
		return fmt.Sprintf("%d %s", status, fit(detail))
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Sprintf("%d status code, unreadable body", status)
	}

	return fmt.Sprintf("%d %s", status, fit(string(encoded)))
}

func describeRedirect(status int, location string) string {
	const refusal = ", which onesie does not follow, since the request carries the API key. " +
		"Point the base URL at where the API is"

	// Only the origin, since the rest of a location may carry a token.
	parsed, err := url.Parse(location)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Sprintf("%d redirect", status) + refusal
	}

	return fmt.Sprintf("%d redirect to %s", status, printable(shownOf(parsed))) + refusal
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

// RetryAfterError reports a server requested delay onesie refused to wait out, because spending the
// remaining retries on a known answer is worse than failing now.
type RetryAfterError struct {
	APIError

	RetryAfter time.Duration
	Cap        time.Duration
}

// APIError is a non 2xx response from the API. Match it with errors.Is against a sentinel, or
// recover it with errors.As to read the status, body and request ID.
type APIError struct {
	Status     int
	RequestID  string
	Header     http.Header
	RetryAfter time.Duration

	// Body is the parsed JSON body, nil when the body is not JSON or is null. RawBody is the body as
	// it arrived.
	Body    any
	RawBody []byte

	// Code is the provider's machine readable error code, a number written in decimal, or empty.
	Code string

	message  string
	sentinel error
}

// Error is Reason with the package prefix.
func (e *APIError) Error() string {
	return "onesie: " + e.Reason()
}

// Reason returns the status and the message read from the response body.
func (e *APIError) Reason() string {
	return e.message
}

// Is reports whether this error matches one of the package sentinels.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrBadRequest:
		return e.Status == http.StatusBadRequest
	case ErrAuthentication:
		return e.Status == http.StatusUnauthorized || e.sentinel == ErrAuthentication
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
		return e.sentinel != nil && target == e.sentinel
	}
}

// Error is Reason with the package prefix.
func (e *RetryAfterError) Error() string {
	return "onesie: " + e.Reason()
}

// Reason names the delay the server asked for and the cap that rejected it.
func (e *RetryAfterError) Reason() string {
	return fmt.Sprintf("status %d, the server asked to retry after %s, above the %s cap", e.Status, e.RetryAfter, e.Cap)
}

// Unwrap returns the embedded APIError, so errors.As and errors.Is reach it.
func (e *RetryAfterError) Unwrap() error {
	return &e.APIError
}

// TimeoutError is a ConnectionError whose cause was a deadline. It unwraps to
// context.DeadlineExceeded, so errors.Is against a caller's own deadline matches it too.
type TimeoutError struct {
	ConnectionError

	Timeout time.Duration
}

// ConnectionError is a transport failure, a cancelled call or a body that stopped arriving.
type ConnectionError struct {
	Err error
}

// Error is Reason with the package prefix.
func (e *ConnectionError) Error() string {
	return "onesie: " + e.Reason()
}

// Reason describes the transport failure.
func (e *ConnectionError) Reason() string {
	if e.Err == nil {
		return "connection error"
	}

	return "connection error: " + e.Err.Error()
}

// Unwrap returns the underlying transport error.
func (e *ConnectionError) Unwrap() error {
	return e.Err
}

// Is matches ErrConnection.
func (e *ConnectionError) Is(target error) bool {
	return target == ErrConnection
}

// Error is Reason with the package prefix.
func (e *TimeoutError) Error() string {
	return "onesie: " + e.Reason()
}

// Reason reports the deadline that fired, or only that one fired when it was the caller's own.
func (e *TimeoutError) Reason() string {
	if e.Timeout <= 0 {
		return "request timed out"
	}

	return fmt.Sprintf("request timed out after %s", e.Timeout)
}

// Unwrap returns the embedded ConnectionError, so errors.As reaches it.
func (e *TimeoutError) Unwrap() error {
	return &e.ConnectionError
}

// Is matches both ErrTimeout and ErrConnection.
func (e *TimeoutError) Is(target error) bool {
	return target == ErrConnection || target == ErrTimeout
}

// ResponseError is a 2xx response this client could not use. Err holds the fault, and Usage is
// what the response billed when its answers failed to decode, nil otherwise.
type ResponseError struct {
	Status int
	Body   []byte
	Err    error
	Usage  *Usage
}

// Error is Reason with the package prefix.
func (e *ResponseError) Error() string {
	return "onesie: " + e.Reason()
}

// Reason describes what could not be read.
func (e *ResponseError) Reason() string {
	return fmt.Sprintf("%d response could not be used: %v", e.Status, e.Err)
}

// Unwrap returns the fault.
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

	kind error
}

// Error is Reason with the package prefix.
func (e *ValidationError) Error() string {
	return "onesie: " + e.Reason()
}

// Reason returns Message.
func (e *ValidationError) Reason() string {
	return e.Message
}

// Is matches ErrValidation, and the more specific sentinel the rejection carries.
func (e *ValidationError) Is(target error) bool {
	return target == ErrValidation || (e.kind != nil && target == e.kind)
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
		ValidationError: ValidationError{Message: message, kind: ErrNotServed},
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

// Error is Reason with the package prefix.
func (e *AnswerError) Error() string {
	return "onesie: " + e.Reason()
}

// Reason says whether the answer was absent or of the wrong type.
func (e *AnswerError) Reason() string {
	if e.Missing {
		return fmt.Sprintf("no answer named %q", e.Name)
	}

	return fmt.Sprintf("answer %q is a %s, not a %s", e.Name, e.Got, e.Want)
}

type resultShapeError struct {
	answer  string
	field   string
	fault   shapeFault
	want    jsonKind
	got     jsonKind
	gotType string
	cause   error
}

type shapeFault int

type jsonKind int

func (e *resultShapeError) Unwrap() error {
	return e.cause
}

func (e *resultShapeError) Error() string {
	field := printable(e.field)

	if e.fault == faultNoAnswer {
		return "no answer for question '" + printable(e.answer) + "'"
	}

	if e.answer == "" {
		return resultSentence(field, e.fault, e.want, e.got)
	}

	name := "answer '" + printable(e.answer) + "'"
	if field == "" {
		return fmt.Sprintf("%s is %s, not %s", name, e.got.phrase(), e.want.phrase())
	}

	phrase, bare, verb := fieldPhrase(field)

	switch e.fault {
	case faultMissing:
		return fmt.Sprintf("%s has no %s, or it is null", name, bare)
	case faultUnknownType:
		return fmt.Sprintf("%s has the type '%s', which is not %s", name, printable(e.gotType), answerKinds)
	case faultEmpty:
		return fmt.Sprintf("%s has %s that %s empty", name, phrase, verb)
	default:
		return fmt.Sprintf("%s has %s that %s not %s", name, phrase, verb, e.want.phrase())
	}
}

func resultSentence(field string, fault shapeFault, want, got jsonKind) string {
	const lead = "the response is not a System One result"

	switch {
	case field == "":
		return lead + ", since it is " + got.phrase()
	case fault == faultMissing:
		return lead + ", since it has no " + field + ", or it is null"
	default:
		_, _, verb := fieldPhrase(field)

		return lead + ", since its " + field + " " + verb + " not " + want.phrase()
	}
}

func (k jsonKind) phrase() string {
	switch k {
	case kindObject:
		return "an object"
	case kindArray:
		return "an array"
	case kindString:
		return "a string"
	case kindNumber:
		return "a number"
	case kindBoolean:
		return "a boolean"
	case kindNull:
		return "null"
	default:
		return "nothing"
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
