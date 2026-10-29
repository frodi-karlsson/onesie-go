package onesie

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
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
	detail   string
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

// Detail returns what the response said beyond its status, or onesie's own note on a redirect or an
// oversized body. It is empty when there is nothing more to say.
func (e *APIError) Detail() string {
	return e.detail
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

// TimeoutError is a ConnectionError whose cause was a deadline. It matches context.DeadlineExceeded
// whichever deadline fired, the caller's, the client's or the transport's.
type TimeoutError struct {
	ConnectionError

	Timeout time.Duration
}

// ConnectionError is a transport failure, a cancelled call or a body that stopped arriving.
type ConnectionError struct {
	Err error

	certificate string
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
			return "could not reach " + parsed.Host + throughProxy(failed.Err) + ": " + e.cause(failed.Err)
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

// Reason reports the client's deadline that fired, or else the host and port the request went to,
// and never a URL. A failure with no request reads request timed out.
func (e *TimeoutError) Reason() string {
	if e.Timeout > 0 {
		return fmt.Sprintf("request timed out after %s", e.Timeout)
	}

	if address := requestAddress(e.Err); address != "" {
		return "request to " + address + " timed out"
	}

	return "request timed out"
}

func requestAddress(err error) string {
	var failed *url.Error
	if !errors.As(err, &failed) {
		return ""
	}

	parsed, parseErr := url.Parse(failed.URL)
	if parseErr != nil || parsed.Hostname() == "" {
		return ""
	}

	port := parsed.Port()
	if port == "" {
		port = defaultPort(parsed.Scheme)
	}

	return net.JoinHostPort(parsed.Hostname(), port)
}

func defaultPort(scheme string) string {
	if scheme == "http" {
		return "80"
	}

	return "443"
}

// Unwrap returns the embedded ConnectionError, so errors.As reaches it.
func (e *TimeoutError) Unwrap() error {
	return &e.ConnectionError
}

// Is matches ErrTimeout, ErrConnection and context.DeadlineExceeded.
func (e *TimeoutError) Is(target error) bool {
	return target == ErrConnection || target == ErrTimeout || target == context.DeadlineExceeded
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
