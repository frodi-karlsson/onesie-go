package onesie

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path"
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

	if !c.insecureHTTP {
		if refused := refuseInsecureBaseURL(baseURL, source); refused != nil {
			return nil, refused
		}
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

// BaseURLSource reports whether the base URL came from an option, the environment, an account or
// the default.
func (c *Client) BaseURLSource() BaseURLSource {
	return c.baseURLSource
}

// Provider returns the provider passed to WithProvider, or TypeSafe when none was.
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

// BaseURLRepeatsPath reports the segment a base URL repeats when its last segment is the first
// segment of the default model's API path, which the client appends.
func (c *Client) BaseURLRepeatsPath() (string, bool) {
	first, _, _ := strings.Cut(strings.TrimPrefix(c.provider.route(c.defaultModel).path, "/"), "/")

	parsed, err := url.Parse(c.baseURL)
	if err != nil || first == "" || path.Base(parsed.Path) != first {
		return "", false
	}

	return first, true
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

	sent, err := c.provider.validated(req, cfg.longContext)
	if err != nil {
		return nil, err
	}

	var decoded json.RawMessage

	res, err := c.do(ctx, cfg, call{
		method: http.MethodPost, path: sent.via.path, payload: sent.body, out: &decoded, decode: sent.via.decode,
		via: sent.via, needs: sent.needs.asNeeds(), imagesHeader: sent.images,
	})
	if err != nil {
		return nil, err
	}

	return c.checkedResult(res, decoded, req.Questions)
}

// SendPrepared sends a body PrepareRaw prepared for this client's provider without preparing it again,
// and returns its checked result and the response body as SystemOneRaw returns it.
func (c *Client) SendPrepared(ctx context.Context, body *PreparedBody, opts ...RequestOption) (*Result, json.RawMessage, error) {
	if body == nil {
		return nil, nil, &ValidationError{Message: "the prepared body is nil. Use Provider.PrepareRaw"}
	}

	if body.provider != c.provider.name {
		return nil, nil, &ValidationError{Message: fmt.Sprintf(
			"the body was prepared for the %s provider, and this client sends to %s", body.provider, c.provider.name)}
	}

	cfg, err := c.resolve(opts)
	if err != nil {
		return nil, nil, err
	}

	if cfg.longContext {
		return nil, nil, &ValidationError{Message: "long context is fixed when the body is prepared. Pass WithLongContext to PrepareRaw"}
	}

	var decoded json.RawMessage

	res, err := c.do(ctx, cfg, call{
		method: http.MethodPost, path: body.sent.via.path, payload: body.sent.body, out: &decoded, decode: body.sent.via.decode,
		via: body.sent.via, needs: body.sent.needs.asNeeds(), imagesHeader: body.sent.images,
	})
	if err != nil {
		return nil, nil, err
	}

	result, err := c.checkedResult(res, decoded, body.asked)
	if err != nil {
		return nil, nil, err
	}

	return result, decoded, nil
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

func (c *Client) checkedResult(res *rawResponse, decoded json.RawMessage, questions Questions) (*Result, error) {
	result := &Result{}

	err := json.Unmarshal(decoded, result)
	if err == nil {
		err = result.Check(questions)
	}

	if err != nil {
		return nil, &ResponseError{Status: res.status, Body: res.body, Err: err, Usage: billedUsage(decoded)}
	}

	result.RequestID = res.header.Get(c.provider.requestIDHeader)
	result.Header = res.header

	return result, nil
}

func billedUsage(decoded []byte) *Usage {
	var billed struct {
		Usage *Usage `json:"usage"`
	}

	if json.Unmarshal(decoded, &billed) != nil {
		return nil
	}

	return billed.Usage
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

// MarshalQuestionsBody encodes req's model and questions with no state key, since a null state is a
// state. LongContext adds long_context, and the assets are left out.
func MarshalQuestionsBody(req Request) ([]byte, error) {
	return json.Marshal(struct {
		Model       string    `json:"model"`
		Questions   Questions `json:"questions"`
		LongContext bool      `json:"long_context,omitempty"`
	}{Model: req.Model, Questions: req.Questions, LongContext: req.LongContext})
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

// SystemOneRaw prepares body as PrepareRaw does with the client's default model, sends it through the
// SystemOne retry loop and returns the response body unchecked, as the route decoded it.
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

	sent := prepared.sent

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

// ModelCard is one model or alias a provider lists. ReleaseDate is a day such as 2026-08-01, or
// empty when the provider gives none.
type ModelCard struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate string `json:"release_date"`
}
