package onesie

import (
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
)

const (
	kindNothing jsonKind = iota
	kindObject
	kindArray
	kindString
	kindNumber
	kindFiniteNumber
	kindWholeNumber
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
	// ErrBaseURLUserInfo matches the BaseURLError for a base URL holding @, where a key pasted into
	// the URL would sit.
	ErrBaseURLUserInfo = errors.New("base URL holds userinfo")
)

// NewAPIError builds the APIError a live response with this status and body gets from p's route for
// model, sentinels included, for fakes and mocks. A blank model is p's built in default.
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
		sentinel:   via.classify(status, parsed, described.whole(), needs),
	}
}

func describe(status int, header http.Header, body any, raw []byte, read func(any) string) description {
	switch {
	case redirected(status, header):
		return description{lead: describeRedirect(status, header.Get("Location"))}
	case body == nil && len(raw) == 0:
		return description{lead: fmt.Sprintf("%d status code, no body", status)}
	case body == nil:
		return describeText(status, string(raw))
	}

	if text, isText := body.(string); isText {
		return describeString(status, text)
	}

	if detail := read(body); detail != "" {
		return description{lead: fmt.Sprintf("%d ", status), detail: detail}
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return description{lead: fmt.Sprintf("%d status code, unreadable body", status)}
	}

	return description{lead: fmt.Sprintf("%d ", status), detail: string(encoded)}
}

type description struct {
	lead   string
	detail string
}

func (d description) whole() string {
	return d.lead + d.detail
}

func (d description) fitted() string {
	// Only the detail is cut, since it comes from the body, so a hostile body cannot reach a log line
	// at full length.
	return d.lead + truncate(d.detail)
}

func redirected(status int, header http.Header) bool {
	return status >= 300 && status < 400 && header.Get("Location") != ""
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

func firstLine(body string) string {
	line, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(body), byteOrderMark), "\n")

	return strings.TrimSpace(withoutControls(line))
}

func printable(text string) string {
	return truncate(withoutControls(text))
}

func withoutControls(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}

		return r
	}, text)
}

func repeatsStatus(line string, status int) bool {
	text := strings.TrimSuffix(line, ".")
	text = strings.TrimSpace(strings.TrimPrefix(text, strconv.Itoa(status)))

	return strings.EqualFold(text, http.StatusText(status))
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

// Reason names the host and the innermost cause of a failed request, and never a URL, a proxy
// address or a credential. A failure with no request reads connection error.
func (e *ConnectionError) Reason() string {
	var failed *url.Error
	if errors.As(e.Err, &failed) {
		if parsed, err := url.Parse(failed.URL); err == nil && parsed.Host != "" {
			return "could not reach " + parsed.Host + throughProxy(failed.Err) + ": " + causeOf(failed.Err)
		}
	}

	if e.Err == nil {
		return "connection error"
	}

	if shown := withoutAddresses(firstLine(e.Err.Error())); shown != "" {
		return "connection error: " + shown
	}

	return "connection error"
}

func throughProxy(err error) string {
	var dial *net.OpError
	for errors.As(err, &dial) {
		if dial.Op == "proxyconnect" {
			return " through a proxy"
		}

		err = dial.Err
	}

	return ""
}

func causeOf(err error) string {
	if problem := certificateProblem(err); problem != "" {
		return problem
	}

	for next := errors.Unwrap(err); next != nil; next = errors.Unwrap(err) {
		err = next
	}

	// The text after the last colon, since the net package writes the address it dialled or the
	// name it looked up before one.
	text, _, _ := strings.Cut(err.Error(), "\n")
	if at := strings.LastIndex(text, ": "); at >= 0 {
		text = text[at+len(": "):]
	}

	if shown := withoutAddresses(firstLine(text)); shown != "" {
		return shown
	}

	return "connection failed"
}

func certificateProblem(err error) string {
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) && invalid.Reason == x509.Expired {
		switch {
		case strings.Contains(invalid.Detail, " is after "):
			return "certificate has expired"
		case strings.Contains(invalid.Detail, " is before "):
			return "certificate is not yet valid"
		}
	}

	for ; err != nil; err = errors.Unwrap(err) {
		if text, isCertificate := strings.CutPrefix(err.Error(), "x509: "); isCertificate {
			problem, _, _ := strings.Cut(firstLine(text), ": ")
			problem, _, _ = strings.Cut(problem, " (")

			return withoutAddresses(problem)
		}
	}

	return ""
}

func withoutAddresses(text string) string {
	kept := make([]string, 0, strings.Count(text, " ")+1)

	for _, token := range strings.Fields(text) {
		if !strings.Contains(token, "://") && !strings.Contains(token, "@") {
			kept = append(kept, token)
		}
	}

	return strings.Join(kept, " ")
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

// Reason reports the client's deadline that fired, or only that one fired when it was the caller's or
// the transport's own.
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
// what the response billed whenever its usage member decoded, nil otherwise.
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
	var fault *AnswerFault
	if errors.As(e.Err, &fault) {
		return fmt.Sprintf("%d response could not be used: %s", e.Status, fault.Reason())
	}

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
		return fmt.Sprintf("no answer named '%s'", printable(e.Name))
	}

	return fmt.Sprintf("answer '%s' is a %s, not a %s", printable(e.Name), e.Got, e.Want)
}

// AnswerFault is a 2xx response that is not a System One result, or whose answer does not fit the
// question asked. Question names the answer to blame, unless WholeResponse says the whole response is.
type AnswerFault struct {
	Question      string
	WholeResponse bool

	field   string
	fault   shapeFault
	want    jsonKind
	got     jsonKind
	gotType string
	text    string
	cause   error
}

type shapeFault int

type jsonKind int

// Error is Reason with the package prefix.
func (e *AnswerFault) Error() string {
	return "onesie: " + e.Reason()
}

// Reason names the answer and what is wrong with it, or what is wrong with the whole response.
func (e *AnswerFault) Reason() string {
	if e.text != "" {
		return e.text
	}

	field := printable(e.field)

	if e.WholeResponse {
		return resultSentence(field, e.fault, e.want, e.got)
	}

	name := "answer '" + printable(e.Question) + "'"
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

// Unwrap returns the decoder's error behind a value of the wrong JSON type, or nil.
func (e *AnswerFault) Unwrap() error {
	return e.cause
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
	case kindFiniteNumber:
		return "a finite number"
	case kindWholeNumber:
		return "a whole number"
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
