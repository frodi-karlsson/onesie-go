package onesie

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
	"unicode/utf8"
)

func TestNewAPIError(t *testing.T) {
	t.Parallel()

	typesafe := TypeSafe().route(jevLatest)
	openrouter := OpenRouter().route(jevLatest)
	gateway := Cloudflare().route(cloudflareJev)
	workers := Cloudflare().route("clef")

	tests := []struct {
		name     string
		via      route
		status   int
		body     string
		sentinel error
		message  string
	}{
		{
			name:     "should map 400 to ErrBadRequest",
			status:   400,
			body:     `{"error":"bad question"}`,
			sentinel: ErrBadRequest,
			message:  "onesie: 400 bad question",
		},
		{
			name:   "should read the nested message of a Berget 404",
			status: 404,
			body: `{"error":{"message":"Model not found: jev-latest","type":"invalid_request_error",` +
				`"code":"model_not_found"}}`,
			sentinel: ErrNotFound,
			message:  "onesie: 404 Model not found: jev-latest",
		},
		{
			name:   "should read the detail of a Levanto 422 for an unknown model",
			status: 422,
			body: `{"detail":[{"loc":["body","model"],"msg":"unknown model 'jev-latest'; GET /v1/models ` +
				`lists the accepted names (levanto-sage-v1.4, sage-latest)","type":"value_error"}]}`,
			sentinel: ErrUnprocessableEntity,
			message: "onesie: 422 model: unknown model 'jev-latest'; GET /v1/models lists the accepted names " +
				"(levanto-sage-v1.4, sage-latest)",
		},
		{
			name:     "should read the detail of a Levanto 401",
			status:   401,
			body:     `{"detail":"API key required. Provide a valid Levanto API key."}`,
			sentinel: ErrAuthentication,
			message:  "onesie: 401 API key required. Provide a valid Levanto API key.",
		},
		{
			name:   "should read the nested message of a Berget 401",
			status: 401,
			body: `{"error":{"code":"invalid_api_key","message":"Invalid API key format","param":null,` +
				`"type":"authentication_error"},"trace_id":"6b5d","request_id":"f9c5"}`,
			sentinel: ErrAuthentication,
			message:  "onesie: 401 Invalid API key format",
		},
		{
			name:     "should map 401 to ErrAuthentication",
			status:   401,
			body:     `{"message":"invalid key"}`,
			sentinel: ErrAuthentication,
			message:  "onesie: 401 invalid key",
		},
		{
			name:     "should map 403 to ErrPermissionDenied",
			status:   403,
			body:     `{"detail":"nope"}`,
			sentinel: ErrPermissionDenied,
			message:  "onesie: 403 nope",
		},
		{
			name:     "should map 404 to ErrNotFound",
			status:   404,
			body:     ``,
			sentinel: ErrNotFound,
			message:  "onesie: 404 status code, no body",
		},
		{
			name:     "should map 422 to ErrUnprocessableEntity",
			status:   422,
			body:     `{"detail":[{"loc":["body","questions"],"msg":"field required"}]}`,
			sentinel: ErrUnprocessableEntity,
			message:  "onesie: 422 questions: field required",
		},
		{
			name:     "should map 429 to ErrRateLimit",
			status:   429,
			body:     `{"error":{"message":"slow down"}}`,
			sentinel: ErrRateLimit,
			message:  "onesie: 429 slow down",
		},
		{
			name:     "should map 402 to ErrPaymentRequired",
			status:   402,
			body:     `{"error":{"message":"Insufficient credits","code":402}}`,
			sentinel: ErrPaymentRequired,
			message:  "onesie: 402 Insufficient credits",
		},
		{
			name:     "should keep a plain error message unchanged",
			status:   400,
			body:     `{"error":{"message":"HTTP is not JSON","code":400}}`,
			sentinel: ErrBadRequest,
			message:  "onesie: 400 HTTP is not JSON",
		},
		{
			name:     "should keep the whole text when an HTTP prefix holds no JSON",
			status:   400,
			body:     `{"error":{"message":"HTTP 400: upstream said no","code":400}}`,
			sentinel: ErrBadRequest,
			message:  "onesie: 400 HTTP 400: upstream said no",
		},
		{
			name:     "should map 529 to ErrServer",
			status:   529,
			body:     `overloaded`,
			sentinel: ErrServer,
			message:  "onesie: 529 status code, with a response body that is not JSON: overloaded",
		},
		{
			name:     "should leave an HTML body out of the message",
			status:   502,
			body:     "<!DOCTYPE html>\n<html><body><h1>502 Bad Gateway</h1></body></html>",
			sentinel: ErrServer,
			message:  "onesie: 502 Bad Gateway, with a response body that is not JSON",
		},
		{
			name:     "should leave out a text body that only repeats the status",
			status:   401,
			body:     "401 Unauthorized.\n",
			sentinel: ErrAuthentication,
			message:  "onesie: 401 Unauthorized, with a response body that is not JSON",
		},
		{
			name:     "should show only the first line of a text body",
			status:   503,
			body:     "\n  upstream connect error\x07 \nreset reason: overflow",
			sentinel: ErrServer,
			message:  "onesie: 503 Service Unavailable, with a response body that is not JSON: upstream connect error",
		},
		{
			name:     "should cut a long first line of a text body",
			status:   500,
			body:     strings.Repeat("é", 130),
			sentinel: ErrServer,
			message: "onesie: 500 Internal Server Error, with a response body that is not JSON: " +
				strings.Repeat("é", 100) + "...",
		},
		{
			name:     "should leave out an HTML body that starts with a byte order mark",
			status:   502,
			body:     "\ufeff<!DOCTYPE html><html></html>",
			sentinel: ErrServer,
			message:  "onesie: 502 Bad Gateway, with a response body that is not JSON",
		},
		{
			name:     "should keep a JSON string body as the message",
			status:   429,
			body:     `"slow down"`,
			sentinel: ErrRateLimit,
			message:  "onesie: 429 slow down",
		},
		{
			name:     "should read a cloudflare 401",
			via:      gateway,
			status:   401,
			body:     `{"result":null,"success":false,"errors":[{"code":10000,"message":"Authentication error"}],"messages":[]}`,
			sentinel: ErrAuthentication,
			message:  "onesie: 401 Authentication error",
		},
		{
			name:     "should match ErrAuthentication for a token the models search refuses with a 400",
			via:      gateway,
			status:   400,
			body:     `{"success":false,"errors":[{"code":9106,"message":"Authentication failed (status: 400)"}],"messages":[],"result":null}`,
			sentinel: ErrAuthentication,
			message:  "onesie: 400 Authentication failed (status: 400)",
		},
		{
			name:     "should read a cloudflare 402",
			via:      gateway,
			status:   402,
			body:     `{"errors":[{"message":"Insufficient balance; add money to your gateway or use BYOK","code":2021}],"success":false,"result":{},"messages":[]}`,
			sentinel: ErrPaymentRequired,
			message:  "onesie: 402 Insufficient balance; add money to your gateway or use BYOK",
		},
		{
			name:     "should read a cloudflare 404 for an unknown model",
			via:      gateway,
			status:   404,
			body:     `{"errors":[{"message":"Model not found: typesafe/jev-nope","code":7003}],"success":false,"result":{},"messages":[]}`,
			sentinel: ErrNotFound,
			message:  "onesie: 404 Model not found: typesafe/jev-nope",
		},
		{
			name:   "should trim the gateway's prefix from a forwarded typesafe error",
			via:    gateway,
			status: 400,
			body: `{"errors":[{"message":"Model execution failed (User Input Error): Noul question must have ` +
				`criteria or instructions: c","code":7003}],"success":false,"result":{},"messages":[]}`,
			sentinel: ErrBadRequest,
			message:  "onesie: 400 Noul question must have criteria or instructions: c",
		},
		{
			name:   "should read a forwarded typesafe body inside the message",
			via:    gateway,
			status: 400,
			body: `{"errors":[{"message":"Model execution failed (User Input Error): {\"detail\":{\"error_type\":` +
				`\"max_tokens_exceeded\"}}","code":7003}],"success":false,"result":{},"messages":[]}`,
			sentinel: ErrBadRequest,
			message:  "onesie: 400 max_tokens_exceeded",
		},
		{
			name:   "should trim every AiError prefix",
			via:    workers,
			status: 422,
			body: `{"errors":[{"message":"AiError: AiError: Request body failed validation: images: request ` +
				`exceeds the image count limit (cd32f070-7a36-4ef3-b9d3-a4c01b75bee9)","code":3030}],` +
				`"success":false,"result":{},"messages":[]}`,
			sentinel: ErrUnprocessableEntity,
			message: "onesie: 422 Request body failed validation: images: request exceeds the image count limit " +
				"(cd32f070-7a36-4ef3-b9d3-a4c01b75bee9)",
		},
		{
			name:   "should read a workers ai 400 for an unknown model",
			via:    workers,
			status: 400,
			body: `{"errors":[{"message":"AiError: No such model: No such model @cf/cloudflare/clef-nope or task ` +
				`(72980980-d159-4205-859d-0aca1986d075)","code":5007}],"success":false,"result":{},"messages":[]}`,
			sentinel: ErrBadRequest,
			message: "onesie: 400 No such model: No such model @cf/cloudflare/clef-nope or task " +
				"(72980980-d159-4205-859d-0aca1986d075)",
		},
		{
			name:     "should leave an AiError text in another shape untrimmed",
			via:      workers,
			status:   400,
			body:     `{"error":"AiError: something"}`,
			sentinel: ErrBadRequest,
			message:  "onesie: 400 AiError: something",
		},
		{
			name:     "should read an empty JSON string as the status text",
			status:   400,
			body:     `""`,
			sentinel: ErrBadRequest,
			message:  "onesie: 400 Bad Request",
		},
		{
			name:     "should cut a JSON string message to its first line",
			status:   502,
			body:     `"upstream reset\nsecond line"`,
			sentinel: ErrServer,
			message:  "onesie: 502 upstream reset",
		},
		{
			name:     "should leave out an HTML page sent as a JSON string",
			status:   502,
			body:     `"<!DOCTYPE html><html><body>oops</body></html>"`,
			sentinel: ErrServer,
			message:  "onesie: 502 Bad Gateway",
		},
		{
			name:     "should read a JSON null body as a body that is not JSON",
			status:   500,
			body:     `null`,
			sentinel: ErrServer,
			message:  "onesie: 500 Internal Server Error, with a response body that is not JSON: null",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			via := tc.via
			if via.path == "" {
				via = typesafe
			}

			err := newAPIError(via, Needs{}, tc.status, http.Header{}, []byte(tc.body), time.Now())

			if !errors.Is(err, tc.sentinel) {
				t.Errorf("errors.Is did not match the expected sentinel")
			}

			if err.Error() != tc.message {
				t.Errorf("message\n got: %s\nwant: %s", err.Error(), tc.message)
			}

			var api *APIError
			if !errors.As(err, &api) {
				t.Fatalf("errors.As failed to recover an *APIError")
			}

			if api.Status != tc.status {
				t.Errorf("status got %d, want %d", api.Status, tc.status)
			}
		})
	}
	fixtures := []struct {
		name    string
		file    string
		message string
	}{
		{
			name:    "should report the first OpenRouter validation issue with its path",
			file:    "openrouter-validation.json",
			message: "onesie: 400 questions.q.criteria.false: Invalid input",
		},
		{
			name:    "should unwrap a TypeSafe detail forwarded by OpenRouter",
			file:    "openrouter-detail.json",
			message: "onesie: 400 Too many score levels. Must have at most 10 levels.",
		},
		{
			name:    "should unwrap a TypeSafe error type forwarded by OpenRouter",
			file:    "openrouter-tokens.json",
			message: "onesie: 400 max_tokens_exceeded",
		},
	}

	for _, tc := range fixtures {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatalf("reading the fixture: %v", err)
			}

			got := newAPIError(openrouter, Needs{}, 400, http.Header{}, body, time.Now()).Error()
			if got != tc.message {
				t.Errorf("message\n got: %s\nwant: %s", got, tc.message)
			}
		})
	}

	t.Run("should trim an Ai prefix after an AiError one", func(t *testing.T) {
		t.Parallel()

		body := `{"errors":[{"message":"AiError: Ai: The estimated number of input and maximum output tokens ` +
			`(50025) exceeded this model context window limit (24576). (6c70e61c-5f06-4c89-8345-d375ce1cda94)",` +
			`"code":5021}],"success":false,"result":{},"messages":[]}`

		want := "onesie: 413 The estimated number of input and maximum output tokens (50025) exceeded this " +
			"model context window limit (24576). (6c70e61c-5f06-4c89-8345-d375ce1cda94)"

		if got := newAPIError(workers, Needs{}, 413, http.Header{}, []byte(body), time.Now()).Error(); got != want {
			t.Errorf("message\n got: %s\nwant: %s", got, want)
		}
	})

	for _, tc := range []struct {
		name     string
		provider Provider
		model    string
		needs    Needs
		status   int
		body     string
		reason   string
		sentinel error
		not      error
	}{
		{
			name: "should read a 401 with no body as a live response reads it", provider: TypeSafe(), status: 401,
			reason: "401 status code, no body", sentinel: ErrAuthentication, not: ErrServer,
		},
		{
			name: "should read a 503 with no body as a live response reads it", provider: TypeSafe(), status: 503,
			reason: "503 status code, no body", sentinel: ErrServer, not: ErrAuthentication,
		},
		{
			name: "should read the route's sentinel from the body", provider: TypeSafe(), status: 400,
			body: `{"detail":"Unknown model: jev-0.1"}`, reason: "400 Unknown model: jev-0.1", sentinel: ErrUnknownModel,
		},
		{
			name: "should read a box refusal from the request's needs", provider: TypeSafe(), needs: Needs{Box: true}, status: 400,
			body: `{"detail":"Invalid request"}`, reason: "400 Invalid request", sentinel: ErrNotServed,
		},
		{
			name: "should read the body on the route the model picks", provider: Cloudflare(), model: "clef", status: 400,
			body:   `{"errors":[{"message":"No such model @cf/cloudflare/clef-0","code":5007}],"success":false}`,
			reason: "400 No such model @cf/cloudflare/clef-0", sentinel: ErrUnknownModel,
		},
		{
			name: "should read the default model's route for a blank model", provider: Cloudflare(), status: 400,
			body:   `{"errors":[{"message":"No such model @cf/cloudflare/clef-0","code":5007}],"success":false}`,
			reason: "400 No such model @cf/cloudflare/clef-0", sentinel: ErrBadRequest, not: ErrUnknownModel,
		},
		{
			name: "should read a status alone from a zero provider", status: 429, body: `{"error":"slow down"}`,
			reason: "429 slow down", sentinel: ErrRateLimit,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := NewAPIError(tc.provider, tc.model, tc.needs, tc.status, []byte(tc.body))

			if err.Status != tc.status || err.Reason() != tc.reason || string(err.RawBody) != tc.body {
				t.Errorf("error = %d %q holding %q, want %d %q holding %q", err.Status, err.Reason(), err.RawBody, tc.status, tc.reason, tc.body)
			}

			if !errors.Is(err, tc.sentinel) {
				t.Errorf("error %q did not match %v", err.Reason(), tc.sentinel)
			}

			if tc.not != nil && errors.Is(err, tc.not) {
				t.Errorf("error %q matched %v", err.Reason(), tc.not)
			}
		})
	}

	for _, tc := range []struct {
		name string
		body string
		json bool
	}{
		{name: "should keep a JSON body as parsed", body: `{"error":"bad"}`, json: true},
		{name: "should keep a JSON string body as parsed", body: `"bad"`, json: true},
		{name: "should hold no parsed body for text", body: "bad gateway"},
		{name: "should hold no parsed body for a JSON null", body: "null"},
		{name: "should hold no parsed body for an empty body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := newAPIError(typesafe, Needs{}, 502, http.Header{}, []byte(tc.body), time.Now())

			if string(err.RawBody) != tc.body || (err.Body != nil) != tc.json {
				t.Errorf("RawBody = %q, Body = %v, want %q and parsed %v", err.RawBody, err.Body, tc.body, tc.json)
			}
		})
	}

	t.Run("should only match ErrPaymentRequired for 402", func(t *testing.T) {
		t.Parallel()

		for _, status := range []int{400, 401, 403} {
			err := newAPIError(typesafe, Needs{}, status, http.Header{}, nil, time.Now())
			if errors.Is(err, ErrPaymentRequired) {
				t.Errorf("status %d matched ErrPaymentRequired", status)
			}
		}
	})

	t.Run("should fill the error's fields from the response", func(t *testing.T) {
		t.Parallel()

		t.Run("should read the request id from the header", func(t *testing.T) {
			t.Parallel()

			header := http.Header{"X-Typesafe-Request-Id": {"req_123"}}

			if got := newAPIError(typesafe, Needs{}, 500, header, nil, time.Now()).RequestID; got != "req_123" {
				t.Errorf("request id got %q, want %q", got, "req_123")
			}
		})

		t.Run("should read retry after seconds into a duration", func(t *testing.T) {
			t.Parallel()

			header := http.Header{"Retry-After": {"3"}}

			if got := newAPIError(typesafe, Needs{}, 429, header, nil, time.Now()).RetryAfter; got != 3*time.Second {
				t.Errorf("retry after got %v, want %v", got, 3*time.Second)
			}
		})

		t.Run("should leave retry after zero when the header is absent", func(t *testing.T) {
			t.Parallel()

			if got := newAPIError(typesafe, Needs{}, 429, http.Header{}, nil, time.Now()).RetryAfter; got != 0 {
				t.Errorf("retry after got %v, want 0", got)
			}
		})

		t.Run("should truncate a long plain text body", func(t *testing.T) {
			t.Parallel()

			body := []byte(fmt.Sprintf("%400s", "x"))

			got := newAPIError(typesafe, Needs{}, 500, http.Header{}, body, time.Now()).Error()

			if len(got) > maxBodyInError+30 {
				t.Errorf("message was not truncated, length %d", len(got))
			}
		})

		t.Run("should truncate a long extracted message", func(t *testing.T) {
			t.Parallel()

			body := []byte(fmt.Sprintf(`{"error":"%400s"}`, "x"))

			got := newAPIError(typesafe, Needs{}, 500, http.Header{}, body, time.Now()).Error()

			if len(got) > maxBodyInError+30 {
				t.Errorf("extracted message was not truncated, length %d", len(got))
			}
		})
	})

	for _, tc := range []struct {
		name    string
		via     route
		body    string
		message string
	}{
		{
			name: "should leave an AiError prefix alone outside cloudflare", via: typesafe,
			body: `{"errors":[{"message":"AiError: x","code":1}]}`, message: "onesie: 400 AiError: x",
		},
		{
			name: "should leave a forwarded body alone outside openrouter", via: typesafe,
			body: `{"error":{"message":"HTTP 400: {\"detail\":\"x\"}"}}`, message: `onesie: 400 HTTP 400: {"detail":"x"}`,
		},
		{
			name: "should unwrap a forwarded body on openrouter", via: openrouter,
			body: `{"error":{"message":"HTTP 400: {\"detail\":\"x\"}"}}`, message: "onesie: 400 x",
		},
		{
			name: "should unwrap a forwarded body with no space after the status", via: openrouter,
			body: `{"error":{"message":"HTTP 422:{\"detail\":\"x\"}"}}`, message: "onesie: 400 x",
		},
		{
			name: "should leave a forwarded status that is not a number alone", via: openrouter,
			body: `{"error":{"message":"HTTP abc: {\"detail\":\"x\"}"}}`, message: `onesie: 400 HTTP abc: {"detail":"x"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := newAPIError(tc.via, Needs{}, 400, http.Header{}, []byte(tc.body), time.Now()).Error(); got != tc.message {
				t.Errorf("message = %q, want %q", got, tc.message)
			}
		})
	}

	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{name: "should write a numeric code in decimal", body: `{"errors":[{"message":"No such model","code":5007}]}`, code: "5007"},
		{name: "should keep a string code as given", body: `{"error":{"message":"Model not found","code":"model_not_found"}}`, code: "model_not_found"},
		{name: "should leave the code empty when the body has none", body: `{"detail":"x"}`},
		{name: "should leave the code empty for a body that is not JSON", body: `oops`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := newAPIError(typesafe, Needs{}, 400, http.Header{}, []byte(tc.body), time.Now()).Code; got != tc.code {
				t.Errorf("Code = %q, want %q", got, tc.code)
			}
		})
	}

	t.Run("should read cloudflare's token codes only on cloudflare's routes", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{"errors":[{"code":10000,"message":"Authentication error"}],"success":false}`)

		for _, via := range []route{gateway, workers} {
			if err := newAPIError(via, Needs{}, 400, http.Header{}, body, time.Now()); !errors.Is(err, ErrAuthentication) {
				t.Errorf("error = %v on %s, want ErrAuthentication", err, via.path)
			}
		}

		if err := newAPIError(openrouter, Needs{}, 400, http.Header{}, body, time.Now()); errors.Is(err, ErrAuthentication) {
			t.Errorf("error = %v on openrouter, want no ErrAuthentication", err)
		}
	})
	boxed := Needs{Images: true, Box: true}

	classified := []struct {
		name     string
		file     string
		status   int
		via      route
		needs    Needs
		sentinel error
		reason   string
	}{
		{name: "should read typesafe's unknown model", file: "typesafe-unknown-model.json", status: 400, via: typesafe, sentinel: ErrUnknownModel, reason: "400 Unknown model: onesie-does-not-exist"},
		{name: "should read typesafe's context limit", file: "typesafe-context-too-long.json", status: 400, via: typesafe, sentinel: ErrContextTooLong, reason: "400 max_tokens_exceeded"},
		{name: "should read typesafe's box refusal", file: "typesafe-box-not-served.json", status: 400, via: typesafe, needs: boxed, sentinel: ErrNotServed, reason: "400 Invalid request."},
		{name: "should read typesafe's count refusal", file: "typesafe-question-limit.json", status: 422, via: typesafe, sentinel: ErrQuestionLimit, reason: "422 questions.rate.score.criteria: List should have at most 10 items after validation, not 11"},
		{name: "should read openrouter's unknown model", file: "openrouter-unknown-model.json", status: 400, via: openrouter, sentinel: ErrUnknownModel, reason: "400 Model onesie-does-not-exist does not exist"},
		{name: "should read openrouter's context limit", file: "openrouter-context-too-long.json", status: 400, via: openrouter, sentinel: ErrContextTooLong, reason: "400 max_tokens_exceeded"},
		{name: "should read openrouter's box refusal", file: "openrouter-box-not-served.json", status: 400, via: openrouter, needs: boxed, sentinel: ErrNotServed, reason: "400 Invalid request."},
		{name: "should read openrouter's count refusal", file: "openrouter-question-limit.json", status: 422, via: openrouter, sentinel: ErrQuestionLimit, reason: "422 questions.rate.score.criteria: List should have at most 10 items after validation, not 11"},
		{name: "should read berget's unknown model", file: "berget-unknown-model.json", status: 404, via: Berget().route(bergetModel), sentinel: ErrUnknownModel, reason: "404 Model not found: jev-latest"},
		{name: "should read levanto's unknown model", file: "levanto-unknown-model.json", status: 422, via: Levanto().route(levantoModel), sentinel: ErrUnknownModel, reason: "422 model: unknown model 'jev-latest'; GET /v1/models lists the accepted names (levanto-sage-v1.4, sage-latest)"},
		{name: "should read the gateway's unknown model", file: "gateway-unknown-model.json", status: 404, via: gateway, sentinel: ErrUnknownModel, reason: "404 Model not found: typesafe/jev-nope"},
		{name: "should read the gateway's context limit", file: "gateway-context-too-long.json", status: 400, via: gateway, sentinel: ErrContextTooLong, reason: "400 max_tokens_exceeded"},
		{name: "should read the gateway's box refusal", file: "gateway-box-not-served.json", status: 400, via: gateway, needs: boxed, sentinel: ErrNotServed, reason: "400 Invalid request."},
		{name: "should read the gateway's count refusal", file: "gateway-question-limit.json", status: 400, via: gateway, sentinel: ErrQuestionLimit, reason: "400 questions.rate.score.criteria: List should have at most 10 items after validation, not 11"},
		{name: "should read cloudflare's refused token", file: "cloudflare-auth.json", status: 401, via: gateway, sentinel: ErrAuthentication, reason: "401 Authentication error"},
		{name: "should read the models search's refused token", file: "cloudflare-models-auth.json", status: 400, via: workers, sentinel: ErrAuthentication, reason: "400 Authentication failed (status: 400)"},
		{name: "should read workers ai's unknown model", file: "workers-unknown-model.json", status: 400, via: workers, sentinel: ErrUnknownModel, reason: "400 No such model: No such model @cf/cloudflare/clef-nope or task (72980980-d159-4205-859d-0aca1986d075)"},
	}

	t.Run("should show only the first line of a detail, without control characters, on every route", func(t *testing.T) {
		t.Parallel()

		routes := map[string]route{
			"typesafe": typesafe, "openrouter": openrouter, "berget": Berget().route(bergetModel),
			"levanto": Levanto().route(levantoModel), "gateway": gateway, "workers": workers,
		}
		const hostile = `"a\u001b[31mb\nline2"`
		bodies := []string{`{"error":` + hostile + `}`, `{"error":{"message":` + hostile + `}}`, `{"errors":[{"message":` + hostile + `}]}`}

		for name, via := range routes {
			for _, body := range bodies {
				if got := newAPIError(via, Needs{}, 400, http.Header{}, []byte(body), time.Now()); got.Reason() != "400 a[31mb" {
					t.Errorf("Reason = %q on %s for %s, want %q", got.Reason(), name, body, "400 a[31mb")
				}
			}
		}
	})

	refusals := []error{ErrUnknownModel, ErrContextTooLong, ErrNotServed, ErrQuestionLimit}

	for _, tc := range classified {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body, err := os.ReadFile(filepath.Join("testdata", "errors", tc.file))
			if err != nil {
				t.Fatalf("reading the fixture: %v", err)
			}

			got := newAPIError(tc.via, tc.needs, tc.status, http.Header{}, body, time.Now())
			if !errors.Is(got, tc.sentinel) || got.Reason() != tc.reason {
				t.Fatalf("error = %v, want %v reading %q", got, tc.sentinel, tc.reason)
			}

			for _, other := range refusals {
				if !errors.Is(other, tc.sentinel) && errors.Is(got, other) {
					t.Errorf("error = %v also matched %v", got, other)
				}
			}
		})
	}

	for _, tc := range []struct {
		name   string
		file   string
		status int
		via    route
		needs  Needs
		not    error
	}{
		{name: "should not read berget's wording as an unknown model on openrouter", file: "berget-unknown-model.json", status: 404, via: openrouter, not: ErrUnknownModel},
		{name: "should not read levanto's wording as an unknown model on typesafe", file: "levanto-unknown-model.json", status: 422, via: typesafe, not: ErrUnknownModel},
		{name: "should not read an invalid request as a box refusal without a box", file: "typesafe-box-not-served.json", status: 400, via: typesafe, not: ErrNotServed},
		{name: "should not read an unknown model from a server error", file: "typesafe-unknown-model.json", status: 500, via: typesafe, not: ErrUnknownModel},
		{name: "should not read typesafe's count wording on berget", file: "typesafe-question-limit.json", status: 422, via: Berget().route(bergetModel), not: ErrQuestionLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body, err := os.ReadFile(filepath.Join("testdata", "errors", tc.file))
			if err != nil {
				t.Fatalf("reading the fixture: %v", err)
			}

			if got := newAPIError(tc.via, tc.needs, tc.status, http.Header{}, body, time.Now()); errors.Is(got, tc.not) {
				t.Errorf("error = %v, want no %v", got, tc.not)
			}
		})
	}

	t.Run("should classify on the whole message and show it truncated", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{"detail":"` + strings.Repeat("x", 300) + ` Unknown model"}`)

		got := newAPIError(typesafe, Needs{}, 400, http.Header{}, body, time.Now())
		if !errors.Is(got, ErrUnknownModel) || strings.Contains(got.Error(), "Unknown model") || !strings.HasSuffix(got.Error(), "...") {
			t.Errorf("error = %v, want ErrUnknownModel with a truncated message", got)
		}
	})

	for _, tc := range []struct {
		name     string
		location string
		want     string
	}{
		{
			name: "should show only the scheme and host of a redirect", location: "https://elsewhere.example/v1/models?key=secret#top",
			want: "onesie: 302 redirect to https://elsewhere.example, which onesie does not follow, since the request carries the API key. " +
				"Point the base URL at where the API is",
		},
		{
			name: "should show no location for a relative redirect", location: "/v2/models?key=secret",
			want: "onesie: 302 redirect, which onesie does not follow, since the request carries the API key. Point the base URL at where the API is",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := newAPIError(typesafe, Needs{}, 302, http.Header{"Location": {tc.location}}, nil, time.Now())
			if got.Error() != tc.want {
				t.Errorf("error = %q, want %q", got.Error(), tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name   string
		via    route
		status int
		body   string
		want   bool
	}{
		{name: "should read typesafe's count wording", via: typesafe, status: 400, body: `{"detail":"Too many score levels. Must have at most 10 levels."}`, want: true},
		{name: "should read a question list that is too short", via: typesafe, status: 422, body: `{"detail":[{"type":"too_short","loc":["body","questions","rate","score","criteria"],"msg":"List should have at least 2 items"}]}`, want: true},
		{name: "should read a count refusal past another entry", via: typesafe, status: 422, body: `{"detail":[{"type":"missing","loc":["body","state"],"msg":"Field required"},{"type":"too_long","loc":["body","questions","pick","choice","criteria"],"msg":"Dictionary should have at most 255 items"}]}`, want: true},
		{name: "should not read a length outside the questions", via: typesafe, status: 422, body: `{"detail":[{"type":"too_long","loc":["body","state"],"msg":"String should have at most 10 characters"}]}`},
		{name: "should not read a count refusal from a server error", via: typesafe, status: 500, body: `{"detail":[{"type":"too_long","loc":["body","questions","rate","score","criteria"],"msg":"List should have at most 10 items"}]}`},
		{name: "should not read a structured count refusal on berget", via: Berget().route(bergetModel), status: 422, body: `{"detail":[{"type":"too_long","loc":["body","questions","rate","score","criteria"],"msg":"List should have at most 10 items"}]}`},
		{
			name: "should read a count refusal openrouter sends unwrapped", via: openrouter, status: 422, want: true,
			body: `{"detail":[{"type":"too_long","loc":["body","questions","rate","score","criteria"],"msg":"List should have at most 10 items"}]}`,
		},
		{
			name: "should read a count refusal the gateway sends unwrapped", via: gateway, status: 400, want: true,
			body: `{"detail":[{"type":"too_long","loc":["body","questions","rate","score","criteria"],"msg":"List should have at most 10 items"}]}`,
		},
		{
			name: "should read the gateway's forwarded count refusal", via: gateway, status: 400, want: true,
			body: `{"errors":[{"message":"Model execution failed (User Input Error): {\"detail\":[{\"type\":\"too_long\",\"loc\":[\"body\",\"questions\",\"rate\",\"score\",\"criteria\"],\"msg\":\"List should have at most 10 items\"}]}","code":7003}]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := newAPIError(tc.via, Needs{}, tc.status, http.Header{}, []byte(tc.body), time.Now()); errors.Is(got, ErrQuestionLimit) != tc.want {
				t.Errorf("error = %v, ErrQuestionLimit = %v, want %v", got, !tc.want, tc.want)
			}
		})
	}

	t.Run("should match the first refusal in order when several match", func(t *testing.T) {
		t.Parallel()

		got := newAPIError(typesafe, boxed, 400, http.Header{}, []byte(`{"detail":"Unknown model, max_tokens_exceeded, Invalid request"}`), time.Now())
		if !errors.Is(got, ErrUnknownModel) || errors.Is(got, ErrContextTooLong) || errors.Is(got, ErrNotServed) {
			t.Errorf("error = %v, want ErrUnknownModel alone", got)
		}
	})
}

func TestOversizedAPIError(t *testing.T) {
	t.Parallel()

	t.Run("should classify on the status alone, so no text rule sees the note", func(t *testing.T) {
		t.Parallel()

		var messages []string

		via := route{classifyError: func(r reply) error {
			messages = append(messages, r.message)

			if r.message != "" {
				return ErrContextTooLong
			}

			return nil
		}}

		got := oversizedAPIError(via, Needs{}, http.StatusBadRequest, http.Header{}, 256, time.Now())

		if errors.Is(got, ErrContextTooLong) || !slices.Equal(messages, []string{""}) {
			t.Errorf("error = %v after classifying %q, want one classification with no message", got, messages)
		}

		if !errors.Is(got, ErrBadRequest) {
			t.Errorf("error = %v, want ErrBadRequest from the status", got)
		}
	})
}

func TestOverLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit int64
		want  string
	}{
		{name: "should write the default limit in MiB", limit: DefaultMaxResponseBytes, want: "the response body is over the 8 MiB limit"},
		{name: "should write a whole number of MiB in MiB", limit: 3 << 20, want: "the response body is over the 3 MiB limit"},
		{name: "should write a limit under a MiB in bytes", limit: 1000, want: "the response body is over the 1000 byte limit"},
		{name: "should write a limit that is not a whole number of MiB in bytes", limit: 1<<20 + 1, want: "the response body is over the 1048577 byte limit"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := overLimit(tc.limit); got != tc.want {
				t.Errorf("overLimit(%d) = %q, want %q", tc.limit, got, tc.want)
			}
		})
	}
}

func TestDescribe(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", maxBodyInError+10)

	tests := []struct {
		name   string
		status int
		body   string
		want   description
	}{
		{name: "should lead with the status and keep the read message as the detail", status: 400, body: `{"error":"bad question"}`, want: description{lead: "400 ", detail: "bad question"}},
		{name: "should keep a body that is not JSON as the detail", status: 529, body: "overloaded", want: description{lead: "529 status code, with a response body that is not JSON: ", detail: "overloaded"}},
		{name: "should lead with the whole message when there is no body", status: 500, want: description{lead: "500 status code, no body"}},
		{name: "should keep a body with no message as the detail", status: 418, body: `{"a":1}`, want: description{lead: "418 ", detail: `{"a":1}`, encoded: true}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := describe(tc.status, http.Header{}, decodeBody([]byte(tc.body)), []byte(tc.body), readMessage); got != tc.want {
				t.Errorf("describe = %+v, want %+v", got, tc.want)
			}
		})
	}

	t.Run("should cut the detail and never the lead", func(t *testing.T) {
		t.Parallel()

		got := describe(400, http.Header{}, map[string]any{"error": long}, nil, readMessage)

		if got.whole() != "400 "+long || got.fitted() != "400 "+long[:maxBodyInError]+"..." {
			t.Errorf("whole = %q and fitted = %q, want the detail cut alone", got.whole(), got.fitted())
		}
	})
}

func TestRetryAfterError(t *testing.T) {
	t.Parallel()

	t.Run("should name the status, the requested delay and the cap", func(t *testing.T) {
		t.Parallel()

		err := &RetryAfterError{
			APIError:   APIError{Status: 429},
			RetryAfter: 2 * time.Minute,
			Cap:        time.Minute,
		}

		want := "onesie: status 429, the server asked to retry after 2m0s, above the 1m0s cap"
		if got := err.Error(); got != want {
			t.Errorf("message got %q, want %q", got, want)
		}
	})
}

func TestTimeoutError(t *testing.T) {
	t.Parallel()

	t.Run("should match both ErrTimeout and ErrConnection", func(t *testing.T) {
		t.Parallel()

		err := error(&TimeoutError{Timeout: time.Second})

		if !errors.Is(err, ErrConnection) {
			t.Errorf("a timeout should match ErrConnection")
		}

		if !errors.Is(err, ErrTimeout) {
			t.Errorf("a timeout should match ErrTimeout")
		}
	})

	t.Run("should be recoverable as a ConnectionError", func(t *testing.T) {
		t.Parallel()

		err := error(&TimeoutError{
			ConnectionError: ConnectionError{Err: context.DeadlineExceeded},
			Timeout:         time.Second,
		})

		var connection *ConnectionError
		if !errors.As(err, &connection) {
			t.Fatalf("errors.As should reach the embedded ConnectionError")
		}

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("the unwrap chain should reach the transport error")
		}
	})

	t.Run("should match context.DeadlineExceeded when the transport reported the timeout", func(t *testing.T) {
		t.Parallel()

		transports := map[string]*http.Transport{
			"https": {TLSHandshakeTimeout: 10 * time.Second},
			"http":  {ResponseHeaderTimeout: 10 * time.Second},
		}

		for scheme, transport := range transports {
			err := transportTimeout(t, scheme, transport)

			var timeout *TimeoutError
			if !errors.As(err, &timeout) {
				t.Fatalf("%s error = %v, want a TimeoutError", scheme, err)
			}

			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("%s error = %v, want it to match context.DeadlineExceeded", scheme, err)
			}
		}
	})

	t.Run("should not report a connection error as a timeout", func(t *testing.T) {
		t.Parallel()

		err := error(&ConnectionError{Err: errors.New("dial failed")})

		if errors.Is(err, ErrTimeout) {
			t.Errorf("a connection error should not match ErrTimeout")
		}
	})

	t.Run("should survive a nil cause", func(t *testing.T) {
		t.Parallel()

		if got := (&ConnectionError{}).Error(); got == "" {
			t.Errorf("a nil cause produced an empty message")
		}
	})
}

func transportTimeout(t *testing.T, scheme string, transport *http.Transport) error {
	t.Helper()

	var err error

	synctest.Test(t, func(t *testing.T) {
		transport.DialContext = func(context.Context, string, string) (net.Conn, error) {
			client, server := net.Pipe()
			go discardUntilClosed(server)

			return client, nil
		}

		defer transport.CloseIdleConnections()

		req, buildErr := http.NewRequestWithContext(t.Context(), http.MethodGet, scheme+"://127.0.0.1:8443/v1?key=secret", nil)
		if buildErr != nil {
			t.Fatalf("new request: %v", buildErr)
		}

		res, doErr := (&http.Client{Transport: transport}).Do(req)
		if doErr == nil {
			_ = res.Body.Close()
			t.Fatalf("%s request succeeded, want a timeout", scheme)
		}

		err = classify(context.Background(), context.Background(), doErr, requestConfig{}, time.Now())
	})

	return err
}

func discardUntilClosed(conn net.Conn) {
	_, _ = io.Copy(io.Discard, conn)
	_ = conn.Close()
}

func TestAnswerError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *AnswerError
		want string
	}{
		{
			name: "should report a missing answer",
			err:  &AnswerError{Name: "absent", Missing: true},
			want: `onesie: no answer named 'absent'`,
		},
		{
			name: "should report a type mismatch",
			err:  &AnswerError{Name: "department", Want: "noul", Got: "choice"},
			want: `onesie: answer 'department' is a choice, not a noul`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.err.Error(); got != tc.want {
				t.Errorf("\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	t.Run("should never cut a character in half", func(t *testing.T) {
		t.Parallel()

		got := truncate(strings.Repeat("a", maxBodyInError-1) + "é and more")

		if !utf8.ValidString(got) {
			t.Errorf("truncate returned invalid UTF-8: %q", got)
		}

		if !strings.HasSuffix(got, "...") {
			t.Errorf("truncate = %q, want it to end in ...", got)
		}
	})

	t.Run("should leave a short message as it is", func(t *testing.T) {
		t.Parallel()

		if got := truncate("short"); got != "short" {
			t.Errorf("truncate = %q, want short", got)
		}
	})
}

func TestValidationErrorReason(t *testing.T) {
	t.Parallel()

	t.Run("should give the message", func(t *testing.T) {
		t.Parallel()

		checkReason(t, &ValidationError{Message: "bad"}, "bad")
	})
}

func TestAPIErrorReason(t *testing.T) {
	t.Parallel()

	t.Run("should give the status and the message", func(t *testing.T) {
		t.Parallel()

		checkReason(t, NewAPIError(TypeSafe(), "", Needs{}, 503, []byte(`{"error":"down"}`)), "503 down")
	})
}

func TestAPIErrorDetail(t *testing.T) {
	t.Parallel()

	typesafe := TypeSafe().route(jevLatest)

	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "should give no detail for a response with no body", status: 401},
		{name: "should give no detail for an html page", status: 502, body: "<html><body>Bad Gateway</body></html>"},
		{name: "should give no detail for text that repeats the status", status: 401, body: "Unauthorized\n"},
		{name: "should give no detail for a JSON body with nothing readable", status: 418, body: `{"a":1}`},
		{name: "should give the first line of a text body", status: 401, body: "bad key\nsecond line", want: "bad key"},
		{name: "should give the first line of a JSON string body", status: 502, body: `"upstream timed out\nretry later"`, want: "upstream timed out"},
		{name: "should give a nested message without the status", status: 401, body: `{"error":{"message":"No auth credentials found","code":401}}`, want: "No auth credentials found"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := newAPIError(typesafe, Needs{}, tc.status, http.Header{}, []byte(tc.body), time.Now())
			if got.Detail() != tc.want {
				t.Errorf("Detail = %q, want %q", got.Detail(), tc.want)
			}
		})
	}

	t.Run("should give the first line of a provider's message", func(t *testing.T) {
		t.Parallel()

		openrouter := OpenRouter().route(jevLatest)
		gateway := Cloudflare().route(cloudflareJev)
		workers := Cloudflare().route("clef")
		countDetail := "questions.rate.score.criteria: List should have at most 10 items after validation, not 11"

		recorded := []struct {
			file   string
			status int
			via    route
			want   string
		}{
			{file: "typesafe-unknown-model.json", status: 400, via: typesafe, want: "Unknown model: onesie-does-not-exist"},
			{file: "typesafe-context-too-long.json", status: 400, via: typesafe, want: "max_tokens_exceeded"},
			{file: "typesafe-box-not-served.json", status: 400, via: typesafe, want: "Invalid request."},
			{file: "typesafe-question-limit.json", status: 422, via: typesafe, want: countDetail},
			{file: "openrouter-unknown-model.json", status: 400, via: openrouter, want: "Model onesie-does-not-exist does not exist"},
			{file: "openrouter-context-too-long.json", status: 400, via: openrouter, want: "max_tokens_exceeded"},
			{file: "openrouter-box-not-served.json", status: 400, via: openrouter, want: "Invalid request."},
			{file: "openrouter-question-limit.json", status: 422, via: openrouter, want: countDetail},
			{file: "berget-unknown-model.json", status: 404, via: Berget().route(bergetModel), want: "Model not found: jev-latest"},
			{
				file: "levanto-unknown-model.json", status: 422, via: Levanto().route(levantoModel),
				want: "model: unknown model 'jev-latest'; GET /v1/models lists the accepted names (levanto-sage-v1.4, sage-latest)",
			},
			{file: "gateway-unknown-model.json", status: 404, via: gateway, want: "Model not found: typesafe/jev-nope"},
			{file: "gateway-context-too-long.json", status: 400, via: gateway, want: "max_tokens_exceeded"},
			{file: "gateway-box-not-served.json", status: 400, via: gateway, want: "Invalid request."},
			{file: "gateway-question-limit.json", status: 400, via: gateway, want: countDetail},
			{file: "cloudflare-auth.json", status: 401, via: gateway, want: "Authentication error"},
			{file: "cloudflare-models-auth.json", status: 400, via: workers, want: "Authentication failed (status: 400)"},
			{
				file: "workers-unknown-model.json", status: 400, via: workers,
				want: "No such model: No such model @cf/cloudflare/clef-nope or task (72980980-d159-4205-859d-0aca1986d075)",
			},
		}

		for _, tc := range recorded {
			body, err := os.ReadFile(filepath.Join("testdata", "errors", tc.file))
			if err != nil {
				t.Fatalf("reading the fixture: %v", err)
			}

			if got := newAPIError(tc.via, Needs{}, tc.status, http.Header{}, body, time.Now()).Detail(); got != tc.want {
				t.Errorf("Detail = %q for %s, want %q", got, tc.file, tc.want)
			}
		}
	})

	t.Run("should give the over limit text for an oversized body", func(t *testing.T) {
		t.Parallel()

		got := oversizedAPIError(typesafe, Needs{}, 502, http.Header{}, DefaultMaxResponseBytes, time.Now())
		if want := "The response body is over the 8 MiB limit"; got.Detail() != want {
			t.Errorf("Detail = %q, want %q", got.Detail(), want)
		}
	})

	const refusal = ", which onesie does not follow, since the request carries the API key. Point the base URL at where the API is"

	redirects := []struct {
		name     string
		location string
		want     string
	}{
		{name: "should give the target and the advice of a redirect", location: "https://elsewhere.example/v1/models?key=secret", want: "Redirect to https://elsewhere.example" + refusal},
		{name: "should give the advice of a relative redirect", location: "/v2/models?key=secret", want: "Redirect" + refusal},
	}

	for _, tc := range redirects {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := newAPIError(typesafe, Needs{}, 302, http.Header{"Location": {tc.location}}, nil, time.Now())
			if got.Detail() != tc.want {
				t.Errorf("Detail = %q, want %q", got.Detail(), tc.want)
			}
		})
	}

	t.Run("should give the redirect and the over limit text for an oversized redirect", func(t *testing.T) {
		t.Parallel()

		header := http.Header{"Location": {"https://elsewhere.example/v1"}}
		want := "Redirect to https://elsewhere.example" + refusal + ", and the response body is over the 8 MiB limit"

		got := oversizedAPIError(typesafe, Needs{}, 302, header, DefaultMaxResponseBytes, time.Now())
		if got.Detail() != want {
			t.Errorf("Detail = %q, want %q", got.Detail(), want)
		}
	})
}

func TestRetryAfterErrorReason(t *testing.T) {
	t.Parallel()

	t.Run("should give the delay and the cap", func(t *testing.T) {
		t.Parallel()

		checkReason(t, &RetryAfterError{APIError: APIError{Status: 429}, RetryAfter: 2 * time.Minute, Cap: time.Minute}, "status 429, the server asked to retry after 2m0s, above the 1m0s cap")
	})
}

func TestResponseErrorReason(t *testing.T) {
	t.Parallel()

	t.Run("should give the fault", func(t *testing.T) {
		t.Parallel()

		checkReason(t, &ResponseError{Status: 200, Err: errors.New("empty response body")}, "200 response could not be used: empty response body")
	})

	t.Run("should give an answer fault without its prefix", func(t *testing.T) {
		t.Parallel()

		fault := (&Result{}).Check(Questions{{ID: "q", Question: Noul{}}})

		checkReason(t, &ResponseError{Status: 200, Err: fault}, "200 response could not be used: question 'q' has no answer")
	})
}

func TestConnectionErrorReason(t *testing.T) {
	t.Parallel()

	proxy := &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 8080}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "should give the cause of an error with no URL", err: errors.New("dial failed"), want: "connection error: dial failed"},
		{
			name: "should name the host and port and the refusal, without the URL or its query",
			err: &url.Error{Op: "Post", URL: "https://api.example:8443/v1/systemone?key=1", Err: &net.OpError{
				Op: "dial", Net: "tcp", Err: errors.New("connection refused"),
			}},
			want: "could not reach api.example:8443: connection refused",
		},
		{
			name: "should give a failed lookup without the name it looked up",
			err: &url.Error{Op: "Post", URL: "https://nothing.invalid/v1/systemone", Err: &net.OpError{
				Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "nothing.invalid", IsNotFound: true},
			}},
			want: "could not reach nothing.invalid: no such host",
		},
		{
			name: "should give an unknown certificate authority without its package",
			err: &url.Error{Op: "Get", URL: "https://api.example/v1/models", Err: &tls.CertificateVerificationError{
				Err: x509.UnknownAuthorityError{},
			}},
			want: "could not reach api.example: certificate signed by unknown authority",
		},
		{
			name: "should never print a proxy address",
			err: &url.Error{Op: "Get", URL: "https://api.example/v1/models", Err: &net.OpError{
				Op: "proxyconnect", Net: "tcp", Err: &net.OpError{Op: "dial", Net: "tcp", Addr: proxy, Err: errors.New("connection refused")},
			}},
			want: "could not reach api.example through a proxy: connection refused",
		},
		{
			name: "should name an expired certificate in general words without the time it checked",
			err: models(&tls.CertificateVerificationError{Err: x509.CertificateInvalidError{
				Reason: x509.Expired, Detail: "current time 2026-10-28T12:00:00Z is after 2026-01-01T00:00:00Z",
			}}),
			want: "could not reach api.example: certificate has expired or is not yet valid",
		},
		{
			name: "should name another certificate problem without its detail",
			err: models(&tls.CertificateVerificationError{Err: x509.CertificateInvalidError{
				Reason: x509.NameConstraintsWithoutSANs, Detail: "ignored detail",
			}}),
			want: "could not reach api.example: issuer has name constraints but leaf doesn't have a SAN extension",
		},
		{
			name: "should drop a URL with credentials from a cause with no colon",
			err:  models(errors.New("proxy http://u:p@proxy.local:3128 refused")),
			want: "could not reach api.example: proxy refused",
		},
		{
			name: "should drop a cause whose tail is a URL",
			err:  models(errors.New("dial: https://api.example/v1/models?key=1")),
			want: "could not reach api.example: connection failed",
		},
		{
			name: "should drop a host with credentials and no scheme",
			err:  models(errors.New("refused by u:p@proxy.local:3128 upstream")),
			want: "could not reach api.example: refused by upstream",
		},
		{
			name: "should give the first line of a joined cause",
			err:  models(errors.Join(errors.New("connection reset"), errors.New("second failure"))),
			want: "could not reach api.example: connection reset",
		},
		{
			name: "should fall back when a cause ends in a colon",
			err:  models(errors.New("dial tcp: ")),
			want: "could not reach api.example: connection failed",
		},
		{
			name: "should drop a URL from an error with no request",
			err:  errors.New("stopped at http://u:p@proxy.local:3128"),
			want: "connection error: stopped at",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			checkReason(t, &ConnectionError{Err: tc.err}, tc.want)
		})
	}
}

func TestTimeoutErrorReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *TimeoutError
		want string
	}{
		{name: "should give the timeout", err: &TimeoutError{ConnectionError: ConnectionError{Err: context.DeadlineExceeded}, Timeout: time.Second}, want: "request timed out after 1s"},
		{name: "should say only that a deadline fired when the timeout is unknown", err: &TimeoutError{ConnectionError: ConnectionError{Err: context.DeadlineExceeded}}, want: "request timed out"},
		{
			name: "should name the host and default port of a request the transport timed out",
			err:  transportTimedOut("https://user:pass@api.example.com/v1/answer?key=secret"), want: "request to api.example.com:443 timed out",
		},
		{name: "should name the port a request gave", err: transportTimedOut("http://localhost:8080/v1"), want: "request to localhost:8080 timed out"},
		{name: "should bracket an IPv6 host", err: transportTimedOut("https://[::1]/v1"), want: "request to [::1]:443 timed out"},
		{
			name: "should give the attempt deadline rather than the host",
			err: &TimeoutError{
				ConnectionError: ConnectionError{Err: &url.Error{Op: "Post", URL: "https://api.example.com/v1", Err: context.DeadlineExceeded}},
				Timeout:         2 * time.Second,
			},
			want: "request timed out after 2s",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			checkReason(t, tc.err, tc.want)
		})
	}

	t.Run("should name the host and port of a TLS handshake timeout from the transport", func(t *testing.T) {
		t.Parallel()

		err := transportTimeout(t, "https", &http.Transport{TLSHandshakeTimeout: 10 * time.Second})

		var timeout *TimeoutError
		if !errors.As(err, &timeout) || !regexp.MustCompile(`^request to 127\.0\.0\.1:8443 timed out$`).MatchString(timeout.Reason()) {
			t.Errorf("error = %v, want it to name the host and port", err)
		}
	})
}

func transportTimedOut(target string) *TimeoutError {
	return &TimeoutError{ConnectionError: ConnectionError{Err: &url.Error{Op: "Post", URL: target, Err: os.ErrDeadlineExceeded}}}
}

func TestAnswerErrorReason(t *testing.T) {
	t.Parallel()

	t.Run("should say the answer is missing", func(t *testing.T) {
		t.Parallel()

		checkReason(t, &AnswerError{Name: "a", Missing: true}, `no answer named 'a'`)
	})
}

func TestCapabilityErrorReason(t *testing.T) {
	t.Parallel()

	t.Run("should give the message", func(t *testing.T) {
		t.Parallel()

		checkReason(t, newCapabilityError("levanto", "m", Needs{Images: true}, nil), "the levanto provider takes no images")
	})
}

func checkReason(t *testing.T, err reasoner, want string) {
	t.Helper()

	if got := err.Reason(); got != want {
		t.Errorf("Reason = %q, want %q", got, want)
	}

	if got := err.Error(); got != "onesie: "+want {
		t.Errorf("Error = %q, want the reason with the prefix", got)
	}
}

type reasoner interface {
	error
	Reason() string
}

func TestAnswerFault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *AnswerFault
		want string
	}{
		{
			name: "should report a body that is not an object",
			err:  &AnswerFault{WholeResponse: true, want: kindObject, got: kindString},
			want: "the response is not a System One result, since it is a string",
		},
		{
			name: "should report a missing top level field",
			err:  &AnswerFault{WholeResponse: true, field: "model", fault: faultMissing, want: kindString},
			want: "the response is not a System One result, since it has no model, or it is null",
		},
		{
			name: "should report a missing answer field",
			err:  &AnswerFault{Question: "x", field: "score", fault: faultMissing, want: kindNumber},
			want: "answer 'x' has no score, or it is null",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			checkReason(t, tc.err, tc.want)
		})
	}

	t.Run("should have no cause for a missing field", func(t *testing.T) {
		t.Parallel()

		var result Result

		err := result.UnmarshalJSON([]byte(`{"answers":{"x":{"type":"noul"}}}`))

		var fault *AnswerFault
		if !errors.As(err, &fault) || fault.Unwrap() != nil {
			t.Errorf("error = %v, want an AnswerFault with no cause", err)
		}
	})
}

func TestDescribeShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "should read an array body's kind from the body", raw: `[1]`, want: "onesie: the response is not a System One result, since it is an array"},
		{name: "should read a number body's kind from the body", raw: `5`, want: "onesie: the response is not a System One result, since it is a number"},
		{name: "should read a string body's kind from the body", raw: `"x"`, want: "onesie: the response is not a System One result, since it is a string"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mistyped := &json.UnmarshalTypeError{Type: reflect.TypeFor[struct{}]()}

			if got := blamingResponse(describeShape([]byte(tc.raw), mistyped)).Error(); got != tc.want {
				t.Errorf("describeShape = %q, want %q", got, tc.want)
			}
		})
	}
}

func models(cause error) error {
	return &url.Error{Op: "Get", URL: "https://api.example/v1/models", Err: cause}
}
