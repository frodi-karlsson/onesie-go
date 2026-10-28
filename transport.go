package onesie

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const retryCountHeader = "X-TypeSafe-Retry-Count"

var errTotalTimeout = errors.New("total timeout")

func (c *Client) do(ctx context.Context, cfg requestConfig, req call) (*rawResponse, error) {
	cfg.attemptTimeout = attemptTimeoutFor(cfg.attemptTimeout, req.needs.Images)

	if cfg.totalTimeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeoutCause(ctx, cfg.totalTimeout, errTotalTimeout)
		defer cancel()
	}

	var payload []byte

	if req.payload != nil {
		compacted, err := compactBody(req.payload)
		if err != nil {
			return nil, err
		}

		payload = compacted
	}

	// Numbered so concurrent requests, and the attempts within one, can be told apart in the logs.
	tag := fmt.Sprintf("#%d %s %s", c.requests.Add(1), req.method, withoutQuery(req.path))
	target := c.baseURL + req.path

	for attempt := 0; ; attempt++ {
		retriesLeft := cfg.retry.MaxRetries - attempt

		res, err := c.attempt(ctx, tag, attempt, target, payload, cfg, req)
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
			return res, decodeInto(res, withoutQuery(target), req.out, req.decode)
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

func compactBody(body []byte) ([]byte, error) {
	// Compacted, not marshalled again. json.Marshal HTML escapes, and a caller that prepared its own
	// body is entitled to have it forwarded as written.
	var flat bytes.Buffer

	if err := json.Compact(&flat, body); err != nil {
		return nil, &ValidationError{Message: "the request body is not JSON: " + err.Error()}
	}

	return flat.Bytes(), nil
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
	target string,
	payload []byte,
	cfg requestConfig,
	sent call,
) (*rawResponse, error) {
	actx, cancel := context.WithTimeout(ctx, cfg.attemptTimeout)
	defer cancel()

	var reader io.Reader
	if payload != nil {
		// A fresh reader per attempt, so a retry can send the body again.
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(actx, sent.method, target, reader)
	if err != nil {
		return nil, &ValidationError{Message: "the request cannot be built: " + err.Error()}
	}

	c.setHeaders(req, cfg.header, sent, attempt, payload != nil)

	if c.logger.Enabled(actx, slog.LevelDebug) {
		c.logger.DebugContext(actx, "onesie request",
			"tag", tag,
			"url", withoutQuery(target),
			"headers", redactedHeader(req.Header),
			// Not redacted, matching the JavaScript SDK, so debug logging a request discloses its
			// payload.
			"body", string(payload),
		)
	}

	started := c.clock.Now()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		failure := classify(ctx, actx, queryHidden(err), cfg)
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
		failure := classify(ctx, actx, err, cfg)
		// Observed, since the server answered, so the attempt counts even though its body was lost.
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

func (c *Client) setHeaders(req *http.Request, header http.Header, sent call, attempt int, hasBody bool) {
	// Caller headers first, so the ones set below cannot be clobbered.
	for name, values := range header {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}

	if sent.imagesHeader {
		req.Header.Set(HeaderImages, "true")
	}

	if sent.needs.LongContext {
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

func decodeInto(res *rawResponse, shown string, out any, decode func([]byte) ([]byte, error)) error {
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
			Err:    fmt.Errorf("the response is not JSON, check the base URL %s", shown),
		}
	}

	body, err := decode(res.body)
	if err != nil {
		return &ResponseError{Status: res.status, Body: res.body, Err: err}
	}

	// Assigned, since json.Unmarshal into a RawMessage drops the whitespace around the value.
	if raw, isRaw := out.(*json.RawMessage); isRaw {
		*raw = body

		return nil
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

func classify(ctx, actx context.Context, err error, cfg requestConfig) error {
	if ctx.Err() != nil {
		return interrupted(ctx, cfg.totalTimeout)
	}

	if actx.Err() != nil {
		return &TimeoutError{ConnectionError: ConnectionError{Err: err}, Timeout: cfg.attemptTimeout}
	}

	return &ConnectionError{Err: err}
}

func queryHidden(err error) error {
	var failed *url.Error
	if errors.As(err, &failed) {
		failed.URL = withoutQuery(failed.URL)
	}

	return err
}

func withoutQuery(target string) string {
	before, _, _ := strings.Cut(target, "?")

	return before
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
