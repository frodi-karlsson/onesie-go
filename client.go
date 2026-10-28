package onesie

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

const systemOnePath = "/v1/systemone"

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

	result := &Result{}

	res, err := c.do(ctx, cfg, call{
		method: http.MethodPost, path: sent.via.path, payload: sent.body, out: result, decode: sent.via.decode,
		via: sent.via, needs: sent.needs.asNeeds(), imagesHeader: sent.images,
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
	method       string
	path         string
	payload      []byte
	out          any
	decode       func([]byte) ([]byte, error)
	via          route
	needs        Needs
	imagesHeader bool
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

// Request is one evaluation, whose questions all see the same state and are answered independently.
// Assets holds the images they reference, and LongContext raises the context limits.
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

	res, err := c.do(ctx, cfg, call{
		method: http.MethodPost, path: sent.via.path, payload: sent.body, decode: identity,
		via: sent.via, needs: sent.needs.asNeeds(), imagesHeader: sent.images,
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
