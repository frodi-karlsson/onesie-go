package onesie

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultAttemptTimeout bounds one HTTP attempt of a request without images, not the whole call.
	DefaultAttemptTimeout = 10 * time.Second
	// AssetAttemptTimeout bounds one HTTP attempt of a request that carries images.
	AssetAttemptTimeout = 60 * time.Second
	// DefaultMaxResponseBytes caps how much of a response body is read.
	DefaultMaxResponseBytes = 8 << 20
	// DefaultMaxRetryAfter is the longest server requested retry delay the client waits out.
	DefaultMaxRetryAfter = time.Minute
	// DefaultRetries is the number of retries after the first attempt.
	DefaultRetries = 2

	// HeaderImages turns on images and Box questions. SystemOne sends it on a request that needs it.
	HeaderImages = "TypeSafe-Danger-Zone-Images"
	// HeaderLongContext raises the context limits. WithLongContext sets it, and WithHeader refuses it.
	HeaderLongContext = "TypeSafe-Danger-Zone-Long-Context"
)

// WithAPIKey sets the API key, so the environment is not read for one. Of WithAPIKey and
// WithAPIKeyFunc, the last one passed wins.
func WithAPIKey(key string) Option {
	return WithAPIKeyFunc(func() (string, error) { return key, nil })
}

// WithAPIKeyFunc supplies the API key, called once by New after every other check passes. Its error
// is returned as it is, and the environment is not read for the key.
func WithAPIKeyFunc(key func() (string, error)) Option {
	return func(c *Client) error {
		if key == nil {
			return &ValidationError{Message: "API key func must not be nil"}
		}

		c.keyFunc = key

		return nil
	}
}

// WithAccountID sets the account for a provider that has accounts, ahead of its account variable.
// A blank id is ignored.
func WithAccountID(id string) Option {
	return func(c *Client) error {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			c.accountID = trimmed
		}

		return nil
	}
}

// WithProvider sets the host the client talks to. The default is TypeSafe.
func WithProvider(p Provider) Option {
	return func(c *Client) error {
		if err := p.check(); err != nil {
			return err
		}

		c.provider = p

		return nil
	}
}

// WithBaseURL sets the API root, taking precedence over the environment.
func WithBaseURL(raw string) Option {
	return func(c *Client) error {
		c.baseURL = raw

		return nil
	}
}

// WithInsecureHTTP lets New take a plain http base URL to a host other than this machine, which
// sends the API key unencrypted.
func WithInsecureHTTP() Option {
	return func(c *Client) error {
		c.insecureHTTP = true

		return nil
	}
}

// WithDefaultModel sets the model used when a request omits one.
func WithDefaultModel(model string) Option {
	return func(c *Client) error {
		c.defaultModel = model

		return nil
	}
}

// WithUserAgent sets the name this client reports. Pass the binary name and its version.
func WithUserAgent(name string) Option {
	return func(c *Client) error {
		if strings.TrimSpace(name) == "" {
			return &ValidationError{Message: "user agent must not be empty"}
		}

		c.userAgent = name

		return nil
	}
}

// WithHTTPClient replaces the transport.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) error {
		if hc == nil {
			return &ValidationError{Message: "http client must not be nil"}
		}

		c.httpClient = hc

		return nil
	}
}

// WithLogger sets the logger. The default discards everything, as a library should.
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) error {
		if l == nil {
			return &ValidationError{Message: "logger must not be nil"}
		}

		c.logger = l

		return nil
	}
}

// WithRetry replaces the retry policy.
func WithRetry(p RetryPolicy) Option {
	return func(c *Client) error {
		if err := p.validate(); err != nil {
			return err
		}

		c.retry = p

		return nil
	}
}

// WithAttemptTimeout bounds every HTTP attempt, with images or without. Retries each get a fresh
// budget, so bound the whole call with WithTotalTimeout or the context.
func WithAttemptTimeout(d time.Duration) Option {
	return func(c *Client) error {
		if d <= 0 {
			return &ValidationError{Message: "attempt timeout must be positive"}
		}

		c.attemptTimeout = d

		return nil
	}
}

// WithTotalTimeout bounds a whole call including every retry and every wait between them. Zero,
// the default, means unbounded apart from the caller's context.
func WithTotalTimeout(d time.Duration) Option {
	return func(c *Client) error {
		if d < 0 {
			return &ValidationError{Message: "total timeout must not be negative"}
		}

		c.totalTimeout = d

		return nil
	}
}

// WithMaxResponseBytes caps how much of a response body is read, so a misbehaving server cannot
// drive this client to allocate without bound.
func WithMaxResponseBytes(n int64) Option {
	return func(c *Client) error {
		if n <= 0 {
			return &ValidationError{Message: "max response bytes must be positive"}
		}

		c.maxResponseBytes = n

		return nil
	}
}

// WithHeader adds a header sent with every request. It cannot override the headers this client sets
// itself, and refuses HeaderLongContext, which WithLongContext sets.
func WithHeader(name, value string) Option {
	return func(c *Client) error {
		if err := refuseLongContextHeader(name); err != nil {
			return err
		}

		c.header.Set(name, value)

		return nil
	}
}

// WithEnv replaces the environment lookup, which is os.LookupEnv by default.
func WithEnv(lookup func(string) (string, bool)) Option {
	return func(c *Client) error {
		if lookup == nil {
			return &ValidationError{Message: "env lookup must not be nil"}
		}

		c.lookupEnv = lookup

		return nil
	}
}

// WithClock replaces the time source, which is the system clock by default.
func WithClock(clock Clock) Option {
	return func(c *Client) error {
		if clock == nil {
			return &ValidationError{Message: "clock must not be nil"}
		}

		c.clock = clock

		return nil
	}
}

// WithRandom replaces the jitter source, which is math/rand/v2's Float64 by default. It must return
// a value from 0 up to but excluding 1.
func WithRandom(random func() float64) Option {
	return func(c *Client) error {
		if random == nil {
			return &ValidationError{Message: "random must not be nil"}
		}

		c.random = random

		return nil
	}
}

// WithAttemptObserver reports every HTTP attempt, so a caller can count retries by status. It is
// called from every goroutine sharing the client, so it must be safe for concurrent use.
func WithAttemptObserver(fn func(Attempt)) Option {
	return func(c *Client) error {
		if fn == nil {
			return &ValidationError{Message: "attempt observer must not be nil"}
		}

		c.observe = fn

		return nil
	}
}

// Option configures a Client at construction.
type Option func(*Client) error

// WithRequestAttemptTimeout overrides the per attempt timeout for one call.
func WithRequestAttemptTimeout(d time.Duration) RequestOption {
	return func(rc *requestConfig) error {
		if d <= 0 {
			return &ValidationError{Message: "attempt timeout must be positive"}
		}

		rc.attemptTimeout = d

		return nil
	}
}

// WithRequestTotalTimeout overrides the total budget for one call. Zero means unbounded.
func WithRequestTotalTimeout(d time.Duration) RequestOption {
	return func(rc *requestConfig) error {
		if d < 0 {
			return &ValidationError{Message: "total timeout must not be negative"}
		}

		rc.totalTimeout = d

		return nil
	}
}

// WithRequestRetry replaces the whole policy for one call. Start from Client.RetryPolicy and
// modify a field, because a Go struct cannot tell an unset field from a zero one.
func WithRequestRetry(p RetryPolicy) RequestOption {
	return func(rc *requestConfig) error {
		if err := p.validate(); err != nil {
			return err
		}

		rc.retry = p

		return nil
	}
}

// WithRequestHeader adds a header to one call. It cannot override the headers this client sets
// itself, and refuses HeaderLongContext, which WithLongContext sets.
func WithRequestHeader(name, value string) RequestOption {
	return func(rc *requestConfig) error {
		if err := refuseLongContextHeader(name); err != nil {
			return err
		}

		rc.header.Set(name, value)

		return nil
	}
}

// WithLongContext raises the context limits for one call.
func WithLongContext() RequestOption {
	return func(rc *requestConfig) error {
		rc.longContext = true

		return nil
	}
}

func refuseLongContextHeader(name string) error {
	if http.CanonicalHeaderKey(name) != http.CanonicalHeaderKey(HeaderLongContext) {
		return nil
	}

	return &ValidationError{Message: "the " + HeaderLongContext + " header is set by WithLongContext, not by hand"}
}

// RequestOption overrides client settings for one call.
type RequestOption func(*requestConfig) error

type requestConfig struct {
	attemptTimeout time.Duration
	totalTimeout   time.Duration
	retry          RetryPolicy
	header         http.Header
	longContext    bool
}

func applyOptions(cfg requestConfig, opts []RequestOption) (requestConfig, error) {
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return requestConfig{}, err
		}
	}

	return cfg, nil
}

func orEnv(value string, lookup func(string) (string, bool), name string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}

	if name == "" {
		return ""
	}

	found, ok := lookup(name)
	if !ok {
		return ""
	}

	return strings.TrimSpace(found)
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}
