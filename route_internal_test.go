package onesie

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const plainAnswer = `{"model":"plain","answers":{"q":{"type":"noul","noul":0.25}},"usage":{"input_tokens":1,"output_tokens":0}}`

func TestRouteRefuse(t *testing.T) {
	t.Parallel()

	const (
		noImages = "onesie: the bare provider takes no images. The typesafe, openrouter and cloudflare providers do"
		noBox    = "onesie: the bare provider takes no box questions. The typesafe and openrouter providers do"
	)

	tests := []struct {
		name    string
		takes   capabilities
		needs   capabilities
		wantErr string
	}{
		{name: "should take what the route takes", takes: takesEverything(), needs: takesEverything()},
		{name: "should take a request that needs nothing", needs: capabilities{}},
		{name: "should refuse images on a route that takes neither", needs: capabilities{images: true}, wantErr: noImages},
		{name: "should refuse a box question on a route that takes neither", needs: capabilities{box: true}, wantErr: noBox},
		{
			name:    "should refuse a box question on a route that takes images only",
			takes:   capabilities{images: true},
			needs:   capabilities{images: true, box: true},
			wantErr: "onesie: the bare provider takes no box questions. The typesafe and openrouter providers do",
		},
		{name: "should take images on a route that takes images only", takes: capabilities{images: true}, needs: capabilities{images: true}},
		{
			name:    "should refuse long context on a route that does not take it",
			takes:   capabilities{images: true, box: true},
			needs:   capabilities{longContext: true},
			wantErr: "onesie: the bare provider takes no long context. The typesafe, openrouter, berget and levanto providers do",
		},
		{
			name:    "should name every capability the route lacks",
			needs:   capabilities{images: true, longContext: true},
			wantErr: "onesie: the bare provider takes no images or long context. The typesafe and openrouter providers do",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := route{takes: tc.takes}.refuse("bare", "m", tc.needs)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}

			if !errors.Is(err, ErrValidation) || err.Error() != tc.wantErr {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}

	t.Run("should leave the refusing provider out of the takers", func(t *testing.T) {
		t.Parallel()

		err := route{takes: capabilities{images: true, box: true}}.refuse("typesafe", "jev-old", capabilities{longContext: true})

		const want = "onesie: the typesafe provider takes no long context. The openrouter, berget and levanto providers do"
		if err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}
	})
}

func TestClientSystemOne(t *testing.T) {
	t.Parallel()

	ask := func(model string) Request {
		return Request{Model: model, State: "s", Questions: Questions{{ID: "q", Question: Noul{Instructions: "q"}}}}
	}

	t.Run("should post to the route the model picks, in its encoding, and read its decoded answers", func(t *testing.T) {
		t.Parallel()

		client, rec := newFakeClient(t, http.StatusOK, `{"result":`+plainAnswer+`}`)

		result, err := client.SystemOne(t.Context(), ask("wrapped"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		sent := rec.only(t)
		if sent.path != "/wrapped" || !strings.HasPrefix(sent.body, `{"input":{"state":"s","model":"wrapped"`) {
			t.Errorf("sent %s %s, want /wrapped with the body under input", sent.path, sent.body)
		}

		noul, err := result.Noul("q")
		if err != nil || noul.Noul != 0.25 {
			t.Errorf("Noul = %v, %v, want 0.25", noul, err)
		}
	})

	t.Run("should post a request with no model to the default model's route", func(t *testing.T) {
		t.Parallel()

		client, rec := newFakeClient(t, http.StatusOK, plainAnswer)

		if _, err := client.SystemOne(t.Context(), ask("")); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if sent := rec.only(t); sent.path != "/plain" || !strings.Contains(sent.body, `"model":"plain"`) {
			t.Errorf("sent %s %s, want /plain naming the default model", sent.path, sent.body)
		}
	})

	img := validImage(t)

	refusals := []struct {
		name    string
		req     Request
		opts    []Option
		reqOpts []RequestOption
		wantErr string
	}{
		{
			name: "should refuse a box question on a route that takes images only",
			req: Request{
				Model: "wrapped", State: "img", Questions: Questions{{ID: "b", Question: Box{Instructions: "the mug"}}},
				Assets: Assets{"img": img},
			},
			wantErr: "onesie: the fake provider takes no box questions. The typesafe and openrouter providers do",
		},
		{
			name: "should refuse images on a route that takes neither",
			req: Request{
				Model: "bare", State: "img", Questions: Questions{{ID: "q", Question: Noul{Instructions: "q"}}},
				Assets: Assets{"img": img},
			},
			wantErr: "onesie: the fake provider takes no images. The typesafe, openrouter and cloudflare providers do",
		},
		{
			name:    "should refuse long context asked for on the request",
			req:     Request{Model: "wrapped", State: "s", Questions: ask("").Questions, LongContext: true},
			wantErr: "onesie: the fake provider takes no long context. The typesafe, openrouter, berget and levanto providers do",
		},
		{
			name:    "should refuse long context asked for by WithLongContext",
			req:     ask("wrapped"),
			reqOpts: []RequestOption{WithLongContext()},
			wantErr: "onesie: the fake provider takes no long context. The typesafe, openrouter, berget and levanto providers do",
		},
		{
			name:    "should report a failing request option rather than refuse",
			req:     ask("wrapped"),
			reqOpts: []RequestOption{WithRequestAttemptTimeout(0), WithLongContext()},
			wantErr: "onesie: attempt timeout must be positive",
		},
		{name: "should report an encode error", req: ask("broken"), wantErr: "encode failed"},
	}

	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, rec := newFakeClient(t, http.StatusOK, plainAnswer, tc.opts...)

			_, err := client.SystemOne(t.Context(), tc.req, tc.reqOpts...)
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}

			if n := rec.count(); n != 0 {
				t.Errorf("sent %d requests, want none", n)
			}
		})
	}

	t.Run("should validate before it refuses", func(t *testing.T) {
		t.Parallel()

		client, rec := newFakeClient(t, http.StatusOK, plainAnswer)

		_, err := client.SystemOne(t.Context(), Request{
			Model: "bare", State: "s", Questions: Questions{{ID: "b", Question: Box{Instructions: "the mug"}}},
		})
		if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "box question 'b' needs an image in the request") {
			t.Fatalf("error = %v, want the box question's own error", err)
		}

		if n := rec.count(); n != 0 {
			t.Errorf("sent %d requests, want none", n)
		}
	})

	unusable := []struct {
		name     string
		response string
	}{
		{name: "should hold the raw body when decode fails", response: `{"nope":1}`},
		{name: "should hold the raw body when the decoded body lacks an answer", response: `{"result":{"model":"w","answers":{},"usage":{}}}`},
	}

	for _, tc := range unusable {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, _ := newFakeClient(t, http.StatusOK, tc.response)

			_, err := client.SystemOne(t.Context(), ask("wrapped"))

			var unusableErr *ResponseError
			if !errors.As(err, &unusableErr) || string(unusableErr.Body) != tc.response {
				t.Fatalf("error = %v, want a ResponseError holding %s", err, tc.response)
			}
		})
	}

	t.Run("should report a question the response did not answer", func(t *testing.T) {
		t.Parallel()

		client, _ := newFakeClient(t, http.StatusOK, `{"model":"p","answers":{},"usage":{"input_tokens":4,"output_tokens":1}}`)

		_, err := client.SystemOne(t.Context(), ask(""))

		var unusable *ResponseError
		if !errors.As(err, &unusable) || err.Error() != "onesie: 200 response could not be used: question 'q' has no answer" {
			t.Fatalf("error = %v, want the missing answer named", err)
		}

		if unusable.Usage == nil || unusable.Usage.InputTokens != 4 {
			t.Errorf("usage = %+v, want the 4 input tokens billed", unusable.Usage)
		}
	})

	t.Run("should not decode an error response", func(t *testing.T) {
		t.Parallel()

		client, _ := newFakeClient(t, http.StatusBadRequest, `{"error":"bad question"}`)

		_, err := client.SystemOne(t.Context(), ask("wrapped"))

		var apiErr *APIError
		if !errors.As(err, &apiErr) || !strings.Contains(err.Error(), "bad question") {
			t.Fatalf("error = %v, want an APIError naming bad question", err)
		}
	})
}

func TestClientSystemOneRaw(t *testing.T) {
	t.Parallel()

	const questions = `"state":"s","questions":{"q":{"type":"noul","instructions":"q"}}`

	t.Run("should post to the route the body's model picks and return the decoded body", func(t *testing.T) {
		t.Parallel()

		client, rec := newFakeClient(t, http.StatusOK, `{"result":`+plainAnswer+`}`)

		got, err := client.SystemOneRaw(t.Context(), json.RawMessage(`{"model":"wrapped",`+questions+`}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if string(got) != plainAnswer {
			t.Errorf("body = %s, want %s", got, plainAnswer)
		}

		if sent := rec.only(t); sent.path != "/wrapped" || sent.body != `{"input":{"model":"wrapped",`+questions+`}}` {
			t.Errorf("sent %s %s, want /wrapped with the body under input", sent.path, sent.body)
		}
	})

	routes := []struct {
		name string
		body string
		want string
	}{
		{name: "should route a trimmed model", body: `{"model":" wrapped ",` + questions + `}`, want: "/wrapped"},
		{name: "should route a body with no model on the default model", body: `{` + questions + `}`, want: "/plain"},
		{name: "should route a blank model on the default model", body: `{"model":"  ",` + questions + `}`, want: "/plain"},
		{name: "should route a top level that is not an object on the default model", body: `[1]`, want: "/plain"},
	}

	for _, tc := range routes {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			response := plainAnswer
			if tc.want == "/wrapped" {
				response = `{"result":` + plainAnswer + `}`
			}

			client, rec := newFakeClient(t, http.StatusOK, response)

			if _, err := client.SystemOneRaw(t.Context(), json.RawMessage(tc.body)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if sent := rec.only(t); sent.path != tc.want {
				t.Errorf("path = %s, want %s", sent.path, tc.want)
			}
		})
	}

	refusals := []struct {
		name    string
		body    string
		opts    []Option
		reqOpts []RequestOption
		wantErr string
	}{
		{
			name:    "should refuse a box question on a route that takes images only",
			body:    `{"model":"wrapped","state":"s","questions":{"b":{"type":"box","instructions":"the mug"}}}`,
			wantErr: "onesie: the fake provider takes no box questions. The typesafe and openrouter providers do",
		},
		{
			name:    "should refuse images on a route that takes neither",
			body:    `{"model":"bare","state":"img","questions":{},"assets":{"img":{"format":"png","data_base64":"AQ=="}}}`,
			wantErr: "onesie: the fake provider takes no images. The typesafe, openrouter and cloudflare providers do",
		},
		{
			name:    "should refuse long context the body asks for",
			body:    `{"model":"wrapped",` + questions + `,"long_context":true}`,
			wantErr: "onesie: the fake provider takes no long context. The typesafe, openrouter, berget and levanto providers do",
		},
		{
			name:    "should refuse long context asked for by WithLongContext",
			body:    `{"model":"wrapped",` + questions + `}`,
			reqOpts: []RequestOption{WithLongContext()},
			wantErr: "onesie: the fake provider takes no long context. The typesafe, openrouter, berget and levanto providers do",
		},
		{
			name:    "should report a failing request option rather than refuse",
			body:    `{"model":"wrapped",` + questions + `}`,
			reqOpts: []RequestOption{WithRequestAttemptTimeout(0), WithLongContext()},
			wantErr: "onesie: attempt timeout must be positive",
		},
		{name: "should report an encode error", body: `{"model":"broken",` + questions + `}`, wantErr: "encode failed"},
		{name: "should refuse a model that is not a string", body: `{"model":1,` + questions + `}`, wantErr: "onesie: model must be a string, got a number"},
	}

	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, rec := newFakeClient(t, http.StatusOK, plainAnswer, tc.opts...)

			_, err := client.SystemOneRaw(t.Context(), json.RawMessage(tc.body), tc.reqOpts...)
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}

			if n := rec.count(); n != 0 {
				t.Errorf("sent %d requests, want none", n)
			}
		})
	}

	t.Run("should check the images before it refuses", func(t *testing.T) {
		t.Parallel()

		client, rec := newFakeClient(t, http.StatusOK, plainAnswer)

		_, err := client.SystemOneRaw(t.Context(), json.RawMessage(
			`{"model":"bare","state":"img","questions":{},"assets":{"img":{"format":"gif","data_base64":"AQ=="}}}`))
		if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "image 'img' has format 'gif', and an image must be png or jpg") {
			t.Fatalf("error = %v, want the asset's own error", err)
		}

		if n := rec.count(); n != 0 {
			t.Errorf("sent %d requests, want none", n)
		}
	})

	t.Run("should hold the raw body when decode fails", func(t *testing.T) {
		t.Parallel()

		client, _ := newFakeClient(t, http.StatusOK, `{"nope":1}`)

		_, err := client.SystemOneRaw(t.Context(), json.RawMessage(`{"model":"wrapped",`+questions+`}`))

		var unusableErr *ResponseError
		if !errors.As(err, &unusableErr) || string(unusableErr.Body) != `{"nope":1}` {
			t.Fatalf("error = %v, want a ResponseError holding the raw body", err)
		}
	})

	t.Run("should not decode an error response", func(t *testing.T) {
		t.Parallel()

		client, _ := newFakeClient(t, http.StatusBadRequest, `{"error":"bad question"}`)

		_, err := client.SystemOneRaw(t.Context(), json.RawMessage(`{"model":"wrapped",`+questions+`}`))

		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error = %v, want an APIError", err)
		}
	})
}

func newFakeClient(t *testing.T, status int, response string, opts ...Option) (*Client, *recorder) {
	t.Helper()

	rec := &recorder{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		rec.add(sentRequest{path: req.URL.Path, body: string(body), longContext: req.Header.Get(HeaderLongContext)})
		w.WriteHeader(status)
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(server.Close)

	base := []Option{
		WithEnv(func(string) (string, bool) { return "", false }),
		WithAPIKey("sk-test"),
		WithBaseURL(server.URL),
		WithProvider(fakeProvider()),
	}

	client, err := New(append(base, opts...)...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return client, rec
}

func fakeProvider() Provider {
	return Provider{
		name:           "fake",
		defaultBaseURL: "http://fake.invalid",
		defaultModel:   "plain",
		modelsPath:     "/models",
		decodeModels:   decodeTypeSafeModels,
		routeFor: func(model string) route {
			switch model {
			case "wrapped":
				return route{
					path:  "/wrapped",
					takes: capabilities{images: true},
					encode: func(b jsonBody) ([]byte, error) {
						body, err := b.bytes()
						if err != nil {
							return nil, err
						}

						return []byte(`{"input":` + string(body) + `}`), nil
					},
					decode: unwrapResult,
				}
			case "bare":
				return route{path: "/bare", encode: encodeAsIs, decode: identity}
			case "broken":
				return route{
					path:   "/broken",
					takes:  takesEverything(),
					encode: func(jsonBody) ([]byte, error) { return nil, errors.New("encode failed") },
					decode: identity,
				}
			default:
				return route{path: "/plain", takes: takesEverything(), encode: encodeAsIs, decode: identity}
			}
		},
	}
}

type recorder struct {
	mu       sync.Mutex
	requests []sentRequest
}

func (r *recorder) add(sent sentRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.requests = append(r.requests, sent)
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.requests)
}

func (r *recorder) only(t *testing.T) sentRequest {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.requests) != 1 {
		t.Fatalf("sent %d requests, want 1", len(r.requests))
	}

	return r.requests[0]
}

type sentRequest struct {
	path        string
	body        string
	longContext string
}

func validImage(t *testing.T) Image {
	t.Helper()

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 320, 320))); err != nil {
		t.Fatalf("encoding the png: %v", err)
	}

	img, err := NewImage(buf.Bytes())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return img
}
