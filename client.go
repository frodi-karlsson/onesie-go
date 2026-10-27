package onesie

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	systemOnePath = "/v1/systemone"

	retryCountHeader = "X-TypeSafe-Retry-Count"
)

var errTotalTimeout = errors.New("total timeout")

// New builds a client. Resolution order is an explicit option, then the environment, then a
// default. Blank values are ignored at every level, except an explicit API key, which is refused.
func New(opts ...Option) (*Client, error) {
	c := &Client{
		httpClient:       &http.Client{},
		logger:           slog.New(slog.DiscardHandler),
		retry:            DefaultRetryPolicy(),
		maxResponseBytes: DefaultMaxResponseBytes,
		header:           http.Header{},
		userAgent:        "onesie-lib",
		lookupEnv:        os.LookupEnv,
		clock:            systemClock{},
		random:           rand.Float64,
		observe:          func(Attempt) {},
		provider:         TypeSafe(),
	}

	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}

	c.httpClient = withoutRedirects(c.httpClient)
	c.defaultModel = c.provider.ResolveModel(c.defaultModel, c.lookupEnv)
	c.runtime = fmt.Sprintf("go/%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)

	baseURL, source, err := c.provider.ResolveBaseURL(c.baseURL, c.accountID, c.lookupEnv)
	if err != nil {
		return nil, err
	}

	if !c.insecureHTTP && InsecureBaseURL(baseURL) {
		return nil, refuseInsecure(baseURL, source)
	}

	c.baseURL, c.baseURLSource = baseURL, source

	key, err := c.resolveKey()
	if err != nil {
		return nil, err
	}

	c.apiKey = key

	return c, nil
}

func (c *Client) resolveKey() (string, error) {
	if c.keyFunc == nil {
		if key := orEnv("", c.lookupEnv, c.provider.envAPIKey); key != "" {
			return key, nil
		}

		return "", &ValidationError{Message: "no API key. Set " + c.provider.envAPIKey, kind: ErrNoAPIKey}
	}

	key, err := c.keyFunc()
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(key) == "" {
		return "", &ValidationError{Message: "the API key is empty", kind: ErrNoAPIKey}
	}

	return strings.TrimSpace(key), nil
}

func withoutRedirects(hc *http.Client) *http.Client {
	// A copy, so a client the caller passed in is left as it was. Go keeps the Authorization header
	// on a redirect to the same domain or a subdomain, which need not be the API.
	refusing := *hc
	refusing.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &refusing
}

// Client talks to the TypeSafe System One API and is safe for concurrent use. Build one with New,
// passing only a Clock, random source or RetryStatus that is safe for concurrent use too.
type Client struct {
	apiKey           string
	keyFunc          func() (string, error)
	accountID        string
	baseURL          string
	baseURLSource    BaseURLSource
	insecureHTTP     bool
	defaultModel     string
	userAgent        string
	runtime          string
	httpClient       *http.Client
	logger           *slog.Logger
	retry            RetryPolicy
	attemptTimeout   time.Duration
	totalTimeout     time.Duration
	maxResponseBytes int64
	header           http.Header
	lookupEnv        func(string) (string, bool)
	clock            Clock
	random           func() float64
	requests         atomic.Uint64
	observe          func(Attempt)
	provider         Provider
}

// BaseURL is the address the client sends every request to, after the environment and the
// default are applied.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// BaseURLSource reports where the client's base URL came from.
func (c *Client) BaseURLSource() BaseURLSource {
	return c.baseURLSource
}

// Provider returns the provider this client talks to.
func (c *Client) Provider() Provider {
	return c.provider
}

// DefaultModel returns the model a request carries when it names none, after the environment is read.
func (c *Client) DefaultModel() string {
	return c.defaultModel
}

// SystemOneURL is the address a question for the default model is posted to, the base URL with
// that model's API path added.
func (c *Client) SystemOneURL() string {
	return c.baseURL + c.provider.route(c.defaultModel).path
}

// RetryPolicy returns a copy, so a caller can modify one field and pass it to WithRequestRetry.
func (c *Client) RetryPolicy() RetryPolicy {
	return c.retry
}

// AttemptTimeout returns the per attempt deadline WithAttemptTimeout set, or DefaultAttemptTimeout
// when none was. A request with images uses AssetAttemptTimeout unless an option sets one.
func (c *Client) AttemptTimeout() time.Duration {
	if c.attemptTimeout == 0 {
		return DefaultAttemptTimeout
	}

	return c.attemptTimeout
}

// VerifyKey lists the models, which proves the key on TypeSafe, Levanto and Cloudflare. Berget also
// answers one small question that spends a few tokens, and OpenRouter also answers its key endpoint.
func (c *Client) VerifyKey(ctx context.Context, opts ...RequestOption) ([]ModelCard, error) {
	models, err := c.ListModels(ctx, opts...)
	if err != nil {
		return nil, err
	}

	if err := c.checkKey(ctx, opts); err != nil {
		return nil, err
	}

	return models, nil
}

func (c *Client) checkKey(ctx context.Context, opts []RequestOption) error {
	switch check := c.provider.keyCheck; check.by {
	case keyCheckByQuestion:
		// Berget validates the body before it looks the key up, so only a request it would answer
		// reaches the key check.
		_, err := c.SystemOne(ctx, Request{
			State:     "onesie key check",
			Questions: Questions{{ID: "key", Question: Noul{Instructions: "The state names a key check"}}},
		}, opts...)

		return err
	case keyCheckByEndpoint:
		cfg, err := c.resolve(opts)
		if err != nil {
			return err
		}

		var discarded json.RawMessage

		_, err = c.do(ctx, cfg, call{
			method: http.MethodGet, path: check.path, out: &discarded, decode: identity,
			via: c.provider.route(c.defaultModel),
		})

		return err
	default:
		return nil
	}
}

// SystemOne answers named questions about a state. Every question is evaluated in parallel and in
// isolation, so batching is cheaper than one call per question.
func (c *Client) SystemOne(ctx context.Context, req Request, opts ...RequestOption) (*Result, error) {
	cfg, err := c.resolve(opts)
	if err != nil {
		return nil, err
	}

	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" {
		req.Model = c.defaultModel
	}

	if err = ValidateRequest(req); err != nil {
		return nil, err
	}

	sent, err := encodeRequest(c.provider, req, cfg.longContext)
	if err != nil {
		return nil, err
	}

	cfg.longContext, cfg.images = sent.needs.longContext, sent.images
	result := &Result{}

	res, err := c.do(ctx, cfg, call{
		method: http.MethodPost, path: sent.via.path, payload: sent.body, out: result, decode: sent.via.decode,
		via: sent.via, needs: sent.needs.asNeeds(),
	})
	if err != nil {
		return nil, withUsage(err, &result.Usage)
	}

	result.RequestID = res.header.Get(c.provider.requestIDHeader)
	result.Header = res.header

	for _, named := range req.Questions {
		if _, ok := result.Answers[named.ID]; !ok {
			return nil, &ResponseError{
				Status: res.status,
				Body:   res.body,
				Err:    &resultShapeError{answer: named.ID, fault: faultNoAnswer},
				Usage:  &result.Usage,
			}
		}
	}

	return result, nil
}

func (c *Client) resolve(opts []RequestOption) (requestConfig, error) {
	return applyOptions(requestConfig{
		attemptTimeout: c.attemptTimeout,
		totalTimeout:   c.totalTimeout,
		retry:          c.retry,
		header:         c.header.Clone(),
	}, opts)
}

type call struct {
	method  string
	path    string
	payload []byte
	out     any
	decode  func([]byte) ([]byte, error)
	via     route
	needs   Needs
}

func withUsage(err error, usage *Usage) error {
	var (
		unusable *ResponseError
		shape    *resultShapeError
	)

	// An answer is decoded only after the usage, so a fault in one means the usage was read.
	if errors.As(err, &unusable) && errors.As(err, &shape) && shape.answer != "" {
		unusable.Usage = usage
	}

	return err
}

// MarshalBody encodes a request as a raw body SystemOneRaw accepts.
func MarshalBody(req Request) ([]byte, error) {
	return json.Marshal(struct {
		State       any       `json:"state"`
		Model       string    `json:"model"`
		Questions   Questions `json:"questions"`
		LongContext bool      `json:"long_context,omitempty"`
		Assets      Assets    `json:"assets,omitempty"`
	}{State: req.State, Model: req.Model, Questions: req.Questions, LongContext: req.LongContext, Assets: req.Assets})
}

// Request is one evaluation. All questions see the same state and are answered independently.
// Assets are the images the state and questions reference. LongContext raises the context limits
// on a provider that has them.
type Request struct {
	State       any
	Questions   Questions
	Model       string
	Assets      Assets
	LongContext bool
}

// SystemOneRaw prepares body as PrepareRaw does, sends it through the SystemOne retry loop and returns
// the response body unchanged. A body PrepareRaw already prepared is sent the same.
func (c *Client) SystemOneRaw(
	ctx context.Context,
	body json.RawMessage,
	opts ...RequestOption,
) (json.RawMessage, error) {
	cfg, err := c.resolve(opts)
	if err != nil {
		return nil, err
	}

	prepared, err := c.provider.prepare(body, c.defaultModel, cfg.longContext)
	if err != nil {
		return nil, err
	}

	sent, err := encodeBody(prepared.via, prepared.needs, withoutLongContext(prepared.body))
	if err != nil {
		return nil, err
	}

	cfg.longContext, cfg.images = sent.needs.longContext, sent.images

	res, err := c.do(ctx, cfg, call{
		method: http.MethodPost, path: sent.via.path, payload: sent.body, decode: identity,
		via: sent.via, needs: sent.needs.asNeeds(),
	})
	if err != nil {
		return nil, err
	}

	// Decoded here, not in do, so a body that is empty or not JSON comes back as it is.
	decoded, err := sent.via.decode(res.body)
	if err != nil {
		return nil, &ResponseError{Status: res.status, Body: res.body, Err: err}
	}

	return decoded, nil
}

// PrepareRaw is Provider.PrepareRaw with the client's provider and default model.
func (c *Client) PrepareRaw(body json.RawMessage, opts ...RequestOption) (json.RawMessage, error) {
	return c.provider.PrepareRaw(body, c.defaultModel, opts...)
}

// ListModels reports the model names this account may send.
func (c *Client) ListModels(ctx context.Context, opts ...RequestOption) ([]ModelCard, error) {
	cfg, err := c.resolve(opts)
	if err != nil {
		return nil, err
	}

	var raw json.RawMessage

	res, err := c.do(ctx, cfg, call{
		method: http.MethodGet, path: c.provider.modelsPath, out: &raw, decode: identity,
		via: c.provider.route(c.defaultModel),
	})
	if err != nil {
		return nil, err
	}

	models, err := c.provider.decodeModels(res.body)
	if err != nil {
		return nil, &ResponseError{Status: res.status, Body: res.body, Err: err}
	}

	return models, nil
}

// ModelCard describes one model or alias.
type ModelCard struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate string `json:"release_date"`
}

func (c *Client) do(ctx context.Context, cfg requestConfig, req call) (*rawResponse, error) {
	cfg.attemptTimeout = attemptTimeoutFor(cfg.attemptTimeout, req.needs.Images)

	if cfg.totalTimeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeoutCause(ctx, cfg.totalTimeout, errTotalTimeout)
		defer cancel()
	}

	var payload []byte

	if req.payload != nil {
		// Compacted rather than re-marshalled. json.Marshal HTML escapes, and a caller that prepared
		// its own body is entitled to have it forwarded as written.
		var flat bytes.Buffer

		if err := json.Compact(&flat, req.payload); err != nil {
			return nil, &ValidationError{Message: "the request body is not JSON: " + err.Error()}
		}

		payload = flat.Bytes()
	}

	// Numbered so concurrent requests, and the attempts within one, can be told apart in the logs.
	tag := fmt.Sprintf("#%d %s %s", c.requests.Add(1), req.method, req.path)
	url := c.baseURL + req.path

	for attempt := 0; ; attempt++ {
		retriesLeft := cfg.retry.MaxRetries - attempt

		res, err := c.attempt(ctx, tag, attempt, req.method, url, payload, cfg)
		if err != nil {
			if ctx.Err() != nil {
				return nil, interrupted(ctx, cfg.totalTimeout)
			}

			if retriesLeft <= 0 || !retryableError(err, cfg.retry) {
				return nil, err
			}

			if waitErr := c.backOff(ctx, tag, attempt, retriesLeft, err.Error(), nil, cfg); waitErr != nil {
				return nil, waitErr
			}

			continue
		}

		if res.status >= 200 && res.status < 300 {
			return res, decodeInto(res, url, req.out, req.decode)
		}

		apiErr := newAPIError(req.via, req.needs, res.status, res.header, res.body, c.clock.Now())

		if retriesLeft <= 0 || !cfg.retry.RetryStatus(res.status) {
			return nil, apiErr
		}

		// A server asking for longer than the cap has already told us the answer. Spending the
		// remaining retries on onesie's own backoff would just arrive early and fail again.
		if delay, tooLong := retryAfterTooLong(res.header, cfg.retry, c.clock.Now()); tooLong {
			return nil, &RetryAfterError{
				APIError:   *apiErr,
				RetryAfter: delay,
				Cap:        cfg.retry.MaxRetryAfter,
			}
		}

		reason := strconv.Itoa(res.status)
		if waitErr := c.backOff(ctx, tag, attempt, retriesLeft, reason, res.header, cfg); waitErr != nil {
			return nil, waitErr
		}
	}
}

func attemptTimeoutFor(set time.Duration, images bool) time.Duration {
	switch {
	case set > 0:
		return set
	case images:
		return AssetAttemptTimeout
	default:
		return DefaultAttemptTimeout
	}
}

func (c *Client) attempt(
	ctx context.Context,
	tag string,
	attempt int,
	method, url string,
	payload []byte,
	cfg requestConfig,
) (*rawResponse, error) {
	actx, cancel := context.WithTimeout(ctx, cfg.attemptTimeout)
	defer cancel()

	var reader io.Reader
	if payload != nil {
		// A fresh reader per attempt, so a retry can send the body again.
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(actx, method, url, reader)
	if err != nil {
		return nil, &ValidationError{Message: "the request cannot be built: " + err.Error()}
	}

	c.setHeaders(req, cfg, attempt, payload != nil)

	if c.logger.Enabled(actx, slog.LevelDebug) {
		c.logger.DebugContext(actx, "onesie request",
			"tag", tag,
			"url", url,
			"headers", redactedHeader(req.Header),
			// Not redacted, matching the JavaScript SDK, so debug logging a request discloses its
			// payload.
			"body", string(payload),
		)
	}

	started := c.clock.Now()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		failure := classify(ctx, actx, err, cfg.attemptTimeout)
		c.observe(Attempt{Index: attempt, Err: failure, Duration: c.clock.Now().Sub(started), Timeout: cfg.attemptTimeout})

		return nil, failure
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.logger.DebugContext(actx, "onesie closing response body", "tag", tag, "error", closeErr)
		}
	}()

	// Read fully under the attempt deadline, so a slow body cannot outlive the timeout.
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.maxResponseBytes+1))
	if err != nil {
		failure := classify(ctx, actx, err, cfg.attemptTimeout)
		// Observed like any other failed round trip. The request was sent and the server answered,
		// so an observer that skipped it would under count attempts and would see a failure it
		// never saw an attempt for.
		c.observe(Attempt{Index: attempt, Err: failure, Duration: c.clock.Now().Sub(started), Timeout: cfg.attemptTimeout})

		return nil, failure
	}

	if int64(len(data)) > c.maxResponseBytes {
		failure := &ConnectionError{
			Err: fmt.Errorf("response body exceeded %d bytes", c.maxResponseBytes),
		}

		c.observe(Attempt{Index: attempt, Err: failure, Duration: c.clock.Now().Sub(started), Timeout: cfg.attemptTimeout})

		return nil, failure
	}

	c.logger.InfoContext(actx, "onesie response",
		"tag", tag,
		"status", resp.StatusCode,
		"elapsed", c.clock.Now().Sub(started),
		"request_id", resp.Header.Get(c.provider.requestIDHeader),
	)

	c.observe(Attempt{
		Index:    attempt,
		Status:   resp.StatusCode,
		Duration: c.clock.Now().Sub(started),
		Timeout:  cfg.attemptTimeout,
	})

	return &rawResponse{status: resp.StatusCode, header: resp.Header, body: data}, nil
}

// Attempt describes one HTTP round trip and the timeout it ran with. An observer sees every attempt,
// retries included.
type Attempt struct {
	Index    int
	Status   int
	Err      error
	Duration time.Duration
	Timeout  time.Duration
}

func (c *Client) setHeaders(req *http.Request, cfg requestConfig, attempt int, hasBody bool) {
	// Caller headers first, so the ones set below cannot be clobbered.
	for name, values := range cfg.header {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}

	if cfg.images {
		req.Header.Set(HeaderImages, "true")
	}

	if cfg.longContext {
		req.Header.Set(HeaderLongContext, "true")
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("X-TypeSafe-SDK", c.userAgent)
	req.Header.Set("X-TypeSafe-Runtime", c.runtime)

	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	} else {
		req.Header.Del("Content-Type")
	}

	req.Header.Del(retryCountHeader)

	if attempt > 0 {
		req.Header.Set(retryCountHeader, strconv.Itoa(attempt))
	}
}

func (c *Client) backOff(
	ctx context.Context,
	tag string,
	attempt, retriesLeft int,
	reason string,
	header http.Header,
	cfg requestConfig,
) error {
	delay := retryDelay(attempt, header, cfg.retry, c.clock.Now(), c.random)

	c.logger.InfoContext(ctx, "onesie retrying",
		"tag", tag,
		"in", delay,
		"retry", attempt+1,
		"of", attempt+retriesLeft,
		"after", reason,
	)

	if err := c.clock.Sleep(ctx, delay); err != nil {
		if ctx.Err() != nil {
			return interrupted(ctx, cfg.totalTimeout)
		}

		return &ConnectionError{Err: err}
	}

	return nil
}

func interrupted(ctx context.Context, total time.Duration) error {
	err := ctx.Err()
	if !errors.Is(err, context.DeadlineExceeded) {
		return &ConnectionError{Err: err}
	}

	if !errors.Is(context.Cause(ctx), errTotalTimeout) {
		total = 0
	}

	return &TimeoutError{ConnectionError: ConnectionError{Err: err}, Timeout: total}
}

func decodeInto(res *rawResponse, url string, out any, decode func([]byte) ([]byte, error)) error {
	if out == nil {
		return nil
	}

	if len(res.body) == 0 {
		return &ResponseError{Status: res.status, Err: errors.New("empty response body")}
	}

	if !json.Valid(res.body) {
		return &ResponseError{
			Status: res.status,
			Body:   res.body,
			Err:    fmt.Errorf("the response is not JSON, check the base URL %s", url),
		}
	}

	body, err := decode(res.body)
	if err != nil {
		return &ResponseError{Status: res.status, Body: res.body, Err: err}
	}

	if err = json.Unmarshal(body, out); err != nil {
		return &ResponseError{Status: res.status, Body: res.body, Err: err}
	}

	return nil
}

type rawResponse struct {
	status int
	header http.Header
	body   []byte
}

func classify(ctx, actx context.Context, err error, timeout time.Duration) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if actx.Err() != nil {
		return &TimeoutError{ConnectionError: ConnectionError{Err: err}, Timeout: timeout}
	}

	return &ConnectionError{Err: err}
}

func retryableError(err error, policy RetryPolicy) bool {
	var timeout *TimeoutError
	if errors.As(err, &timeout) {
		return policy.RetryTimeout
	}

	var connection *ConnectionError
	if errors.As(err, &connection) {
		return policy.RetryConnection
	}

	return false
}
