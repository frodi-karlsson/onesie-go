package onesie_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/frodi-karlsson/onesie-go"
)

func TestProviderNamed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "should return typesafe for typesafe", input: "typesafe", want: "typesafe"},
		{name: "should return typesafe for an empty name", input: "", want: "typesafe"},
		{name: "should return openrouter for openrouter", input: "openrouter", want: "openrouter"},
		{name: "should return berget for berget", input: "berget", want: "berget"},
		{name: "should return levanto for levanto", input: "levanto", want: "levanto"},
		{name: "should return cloudflare for cloudflare", input: "cloudflare", want: "cloudflare"},
		{name: "should trim the name", input: " openrouter ", want: "openrouter"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			provider, err := onesie.ProviderNamed(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if provider.Name() != tc.want {
				t.Errorf("Name = %q, want %q", provider.Name(), tc.want)
			}
		})
	}

	t.Run("should reject an unknown name and list the valid ones", func(t *testing.T) {
		t.Parallel()

		_, err := onesie.ProviderNamed("nope")
		if !errors.Is(err, onesie.ErrValidation) {
			t.Fatalf("error got %v, want ErrValidation", err)
		}

		const want = "onesie: unknown provider 'nope'. Use typesafe, openrouter, berget, levanto or cloudflare"
		if err.Error() != want {
			t.Errorf("error = %q, want %q", err.Error(), want)
		}
	})
}

func TestProviderRefuse(t *testing.T) {
	t.Parallel()

	const (
		noImages = "onesie: the levanto provider takes no images. The typesafe, openrouter and cloudflare providers do"
		noBox    = "onesie: the levanto provider takes no box questions. The typesafe and openrouter providers do"
	)

	everything := onesie.Needs{Images: true, Box: true, LongContext: true}

	tests := []struct {
		name     string
		provider onesie.Provider
		model    string
		needs    onesie.Needs
		wantErr  string
	}{
		{name: "should take everything on typesafe", provider: onesie.TypeSafe(), needs: everything},
		{name: "should take everything on openrouter", provider: onesie.OpenRouter(), needs: everything},
		{name: "should refuse images on levanto", provider: onesie.Levanto(), needs: onesie.Needs{Images: true}, wantErr: noImages},
		{
			name: "should refuse images on berget", provider: onesie.Berget(), needs: onesie.Needs{Images: true, Box: true},
			wantErr: "onesie: the berget provider takes no images or box questions. The typesafe and openrouter providers do",
		},
		{name: "should take long context on levanto", provider: onesie.Levanto(), needs: onesie.Needs{LongContext: true}},
		{name: "should read a blank model as the default", provider: onesie.Levanto(), model: "  ", needs: onesie.Needs{Box: true}, wantErr: noBox},
		{name: "should take images on cloudflare", provider: onesie.Cloudflare(), needs: onesie.Needs{Images: true}},
		{
			name: "should refuse a box question on cloudflare", provider: onesie.Cloudflare(), model: "clef-flash",
			needs:   onesie.Needs{Images: true, Box: true},
			wantErr: "onesie: the cloudflare provider takes no box questions. The typesafe and openrouter providers do",
		},
		{
			name: "should refuse long context on cloudflare", provider: onesie.Cloudflare(),
			needs:   onesie.Needs{LongContext: true},
			wantErr: "onesie: the cloudflare provider takes no long context. The typesafe, openrouter, berget and levanto providers do",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.provider.Refuse(tc.model, tc.needs)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}

			if !errors.Is(err, onesie.ErrValidation) || err.Error() != tc.wantErr {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestProviderPinned(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider onesie.Provider
		model    string
		want     bool
	}{
		{name: "should pin a three part version on typesafe", provider: onesie.TypeSafe(), model: "jev-1.13.0", want: true},
		{name: "should pin a two part version on typesafe", provider: onesie.TypeSafe(), model: "jev-2.0", want: true},
		{name: "should not pin the latest alias", provider: onesie.TypeSafe(), model: "jev-latest"},
		{name: "should not pin a one part version", provider: onesie.TypeSafe(), model: "jev-1"},
		{name: "should not pin a prerelease", provider: onesie.TypeSafe(), model: "jev-1.13.0-rc.1"},
		{name: "should not pin a four part version", provider: onesie.TypeSafe(), model: "jev-1.2.3.4"},
		{name: "should not pin a bare version", provider: onesie.TypeSafe(), model: "-1.2"},
		{name: "should not pin a name with no version", provider: onesie.TypeSafe(), model: "some-model"},
		{name: "should not pin an empty name", provider: onesie.TypeSafe(), model: ""},
		{name: "should not pin a version on openrouter", provider: onesie.OpenRouter(), model: "typesafe/jev-1.13"},
		{name: "should not pin an alias on openrouter", provider: onesie.OpenRouter(), model: "typesafe/jev-latest"},
		{name: "should not pin a typesafe name on another provider", provider: onesie.OpenRouter(), model: "jev-1.13.0"},
		{name: "should not pin the default alias on berget", provider: onesie.Berget(), model: "systemone"},
		{name: "should not pin a canonical id on berget", provider: onesie.Berget(), model: "Qwen/Qwen3.5-2B"},
		{name: "should not pin a versioned name on berget", provider: onesie.Berget(), model: "jev-1.13.0"},
		{name: "should not pin the default alias on levanto", provider: onesie.Levanto(), model: "sage-latest"},
		{name: "should not pin a versioned name on levanto", provider: onesie.Levanto(), model: "levanto-sage-v1.4"},
		{name: "should not pin jev on cloudflare", provider: onesie.Cloudflare(), model: "typesafe/jev"},
		{name: "should not pin a versioned name on cloudflare", provider: onesie.Cloudflare(), model: "jev-1.13.0"},
		{name: "should not pin anything on a zero provider", model: "jev-1.13.0"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.provider.Pinned(tc.model); got != tc.want {
				t.Errorf("%s Pinned(%q) = %v, want %v", tc.provider.Name(), tc.model, got, tc.want)
			}
		})
	}
}

func TestProviderBoxModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider onesie.Provider
		want     string
	}{
		{name: "should name jev-preview on typesafe", provider: onesie.TypeSafe(), want: "jev-preview"},
		{name: "should name jev-preview on openrouter", provider: onesie.OpenRouter(), want: "jev-preview"},
		{name: "should name no model on berget", provider: onesie.Berget()},
		{name: "should name no model on levanto", provider: onesie.Levanto()},
		{name: "should name no model on cloudflare", provider: onesie.Cloudflare()},
		{name: "should name no model on a zero provider"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.provider.BoxModel(); got != tc.want {
				t.Errorf("BoxModel() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProviderResponseBody(t *testing.T) {
	t.Parallel()

	const answered = `{"model":"m","answers":{"q":{"type":"noul","noul":0.1}},"usage":{"input_tokens":0,"output_tokens":0}}`

	result := &onesie.Result{Model: "m", Answers: map[string]onesie.Answer{"q": &onesie.NoulAnswer{Noul: 0.1}}}

	tests := []struct {
		name     string
		provider onesie.Provider
		model    string
		want     string
	}{
		{name: "should write the result as it is on typesafe", provider: onesie.TypeSafe(), want: answered},
		{name: "should write the result as it is on berget", provider: onesie.Berget(), want: answered},
		{name: "should wrap the result for jev on cloudflare", provider: onesie.Cloudflare(), want: `{"result":` + answered + `}`},
		{name: "should wrap the result for clef on cloudflare", provider: onesie.Cloudflare(), model: "clef", want: `{"result":` + answered + `}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := tc.provider.ResponseBody(tc.model, result)
			if err != nil || string(got) != tc.want {
				t.Errorf("ResponseBody = %s, %v, want %s", got, err, tc.want)
			}
		})
	}

	for _, provider := range onesie.Providers() {
		for model, shown := range map[string]string{"": "the default model", "clef": "clef"} {
			t.Run("should answer a send for "+shown+" on "+provider.Name(), func(t *testing.T) {
				t.Parallel()

				body, err := provider.ResponseBody(model, result)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				transport := &recordingTransport{body: string(body)}
				client, _ := newTestClient(t, "https://api.example", onesie.WithProvider(provider),
					onesie.WithHTTPClient(&http.Client{Transport: transport}))

				got, err := client.SystemOne(t.Context(), onesie.Request{State: "s", Model: model, Questions: oneNoul()})
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if noul, _ := got.Noul("q"); noul == nil || noul.Noul != 0.1 || got.Model != "m" {
					t.Errorf("result = %+v, want q at 0.1 from m", got)
				}
			})
		}
	}

	t.Run("should leave the request id to the header, so it round trips", func(t *testing.T) {
		t.Parallel()

		tagged := &onesie.Result{Model: "m", Answers: result.Answers, RequestID: "req-1"}

		body, err := onesie.TypeSafe().ResponseBody("", tagged)
		if err != nil || strings.Contains(string(body), "request_id") {
			t.Fatalf("ResponseBody = %s, %v, want a body with no request_id", body, err)
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-TypeSafe-Request-Id", tagged.RequestID)
			_, _ = w.Write(body)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL, onesie.WithProvider(onesie.TypeSafe()))

		got, err := client.SystemOne(t.Context(), onesie.Request{State: "s", Questions: oneNoul()})
		if err != nil || got.RequestID != tagged.RequestID {
			t.Fatalf("result = %+v, %v, want request id %s", got, err, tagged.RequestID)
		}
	})

	t.Run("should write a result with no answers as one a send refuses", func(t *testing.T) {
		t.Parallel()

		body, err := onesie.Cloudflare().ResponseBody("", &onesie.Result{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		transport := &recordingTransport{body: string(body)}
		client, _ := newTestClient(t, "https://api.example", onesie.WithProvider(onesie.Cloudflare()),
			onesie.WithHTTPClient(&http.Client{Transport: transport}))

		_, err = client.SystemOne(t.Context(), onesie.Request{State: "s", Questions: oneNoul()})
		if want := "onesie: 200 response could not be used: the response is not a System One result, since it has no answers"; err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}
	})

	for _, tc := range []struct {
		name     string
		provider onesie.Provider
		result   *onesie.Result
	}{
		{name: "should refuse a zero provider", result: result},
		{name: "should refuse a nil result", provider: onesie.TypeSafe()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := tc.provider.ResponseBody("", tc.result); !errors.Is(err, onesie.ErrValidation) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
		})
	}
}

func TestProviders(t *testing.T) {
	t.Parallel()

	t.Run("should list every provider in a fixed order", func(t *testing.T) {
		t.Parallel()

		var names []string
		for _, provider := range onesie.Providers() {
			names = append(names, provider.Name())
		}

		if got := strings.Join(names, " "); got != "typesafe openrouter berget levanto cloudflare" {
			t.Errorf("providers = %s", got)
		}
	})

	t.Run("should list providers ProviderNamed resolves", func(t *testing.T) {
		t.Parallel()

		for _, provider := range onesie.Providers() {
			named, err := onesie.ProviderNamed(provider.Name())
			if err != nil || named.Name() != provider.Name() {
				t.Errorf("ProviderNamed(%s) = %v, %v", provider.Name(), named.Name(), err)
			}
		}
	})
}

func TestCapabilityError(t *testing.T) {
	t.Parallel()

	t.Run("should match ErrNotServed, as a server refusal does", func(t *testing.T) {
		t.Parallel()

		if err := onesie.Levanto().Refuse("", onesie.Needs{Images: true}); !errors.Is(err, onesie.ErrNotServed) {
			t.Errorf("error = %v, want ErrNotServed", err)
		}
	})

	t.Run("should carry the provider, the model, what is missing and who takes it", func(t *testing.T) {
		t.Parallel()

		err := onesie.Cloudflare().Refuse("clef-flash", onesie.Needs{Images: true, Box: true, LongContext: true})

		var refused *onesie.CapabilityError
		if !errors.As(err, &refused) {
			t.Fatalf("error = %v, want a CapabilityError", err)
		}

		if refused.Provider != "cloudflare" || refused.Model != "clef-flash" ||
			refused.Missing != (onesie.Needs{Box: true, LongContext: true}) ||
			strings.Join(refused.Takers, " ") != "typesafe openrouter" {
			t.Errorf("error = %+v", refused)
		}

		const want = "onesie: the cloudflare provider takes no box questions or long context. " +
			"The typesafe and openrouter providers do"
		if err.Error() != want {
			t.Errorf("message = %q, want %q", err.Error(), want)
		}
	})

	t.Run("should name the default model for a blank one", func(t *testing.T) {
		t.Parallel()

		var refused *onesie.CapabilityError
		if err := onesie.Cloudflare().Refuse(" ", onesie.Needs{LongContext: true}); !errors.As(err, &refused) ||
			refused.Model != onesie.Cloudflare().DefaultModel() {
			t.Errorf("error = %+v, want the default model named", err)
		}
	})

	t.Run("should still be a ValidationError", func(t *testing.T) {
		t.Parallel()

		err := onesie.Levanto().Refuse("", onesie.Needs{Images: true})

		var invalid *onesie.ValidationError
		if !errors.As(err, &invalid) || !errors.Is(err, onesie.ErrValidation) || "onesie: "+invalid.Message != err.Error() {
			t.Errorf("error = %v, want a ValidationError with the same message", err)
		}
	})

	t.Run("should reach the caller of SystemOneRaw", func(t *testing.T) {
		t.Parallel()

		client, err := onesie.New(onesie.WithAPIKey("k"), onesie.WithProvider(onesie.Levanto()),
			onesie.WithEnv(func(string) (string, bool) { return "", false }))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = client.SystemOneRaw(t.Context(), []byte(`{"state":"s","questions":{"b":{"type":"box","instructions":"x"}}}`))

		var refused *onesie.CapabilityError
		if !errors.As(err, &refused) || refused.Model != onesie.Levanto().DefaultModel() || !refused.Missing.Box {
			t.Errorf("error = %v, want a CapabilityError for the box", err)
		}
	})
}

func TestProviderCheck(t *testing.T) {
	t.Parallel()

	square, err := onesie.NewImage(squarePNG(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	box := onesie.Questions{{ID: "mug", Question: onesie.Box{Instructions: "the mug in image-a"}}}
	photo := onesie.Request{State: "image-a", Questions: box, Assets: onesie.Assets{"image-a": square}}
	padded := onesie.Image{Format: "png", Data: append(squarePNG(t), bytes.Repeat([]byte{0}, onesie.MaxBodyBytes/1200)...)}
	repeated := onesie.Request{State: strings.Repeat("img-a ", 1000), Questions: oneNoul(), Assets: onesie.Assets{"img-a": padded}}

	tests := []struct {
		name     string
		provider onesie.Provider
		req      onesie.Request
		opts     []onesie.RequestOption
		wantErr  string
	}{
		{name: "should take a request the provider takes", provider: onesie.TypeSafe(), req: photo},
		{
			name: "should refuse a request with no questions", provider: onesie.TypeSafe(), req: onesie.Request{State: "s"},
			wantErr: "onesie: at least one question is required",
		},
		{
			name: "should refuse a box question with no image, which Encode sends", provider: onesie.TypeSafe(),
			req:     onesie.Request{State: "s", Questions: box},
			wantErr: "onesie: box question 'mug' needs an image in the request",
		},
		{
			name: "should refuse what the provider's route does not take", provider: onesie.Levanto(),
			req:     onesie.Request{State: "image-a", Questions: oneNoul(), Assets: onesie.Assets{"image-a": square}},
			wantErr: "onesie: the levanto provider takes no images. The typesafe, openrouter and cloudflare providers do",
		},
		{
			name: "should refuse a blank model as the built in default refuses it", provider: onesie.Cloudflare(),
			req:     onesie.Request{Model: " ", State: "s", Questions: oneNoul(), LongContext: true},
			wantErr: "onesie: the cloudflare provider takes no long context. The typesafe, openrouter, berget and levanto providers do",
		},
		{
			name: "should refuse a body openrouter would inline past the limit", provider: onesie.OpenRouter(), req: repeated,
			wantErr: "This provider takes an image once per place it is named, so name each image once",
		},
		{
			name: "should refuse long context from WithLongContext as SystemOne does", provider: onesie.Cloudflare(),
			req: onesie.Request{State: "s", Questions: oneNoul()}, opts: []onesie.RequestOption{onesie.WithLongContext()},
			wantErr: "onesie: the cloudflare provider takes no long context. The typesafe, openrouter, berget and levanto providers do",
		},
		{
			name: "should take WithLongContext on a provider that takes long context", provider: onesie.TypeSafe(),
			req: onesie.Request{State: "s", Questions: oneNoul()}, opts: []onesie.RequestOption{onesie.WithLongContext()},
		},
		{
			name: "should refuse a failing option", provider: onesie.TypeSafe(), req: onesie.Request{State: "s", Questions: oneNoul()},
			opts: []onesie.RequestOption{onesie.WithRequestAttemptTimeout(0)}, wantErr: "onesie: attempt timeout must be positive",
		},
		{name: "should refuse a zero provider", req: onesie.Request{State: "s", Questions: oneNoul()}, wantErr: "onesie: the provider is empty. Use ProviderNamed with typesafe, openrouter, berget, levanto or cloudflare"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.provider.Check(tc.req, tc.opts...)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}

			if !errors.Is(err, onesie.ErrValidation) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestProviderEncode(t *testing.T) {
	t.Parallel()

	square, err := onesie.NewImage(squarePNG(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	box := onesie.Questions{{ID: "mug", Question: onesie.Box{Instructions: "the mug in `photo`"}}}
	photo := func(model string, questions onesie.Questions) onesie.Request {
		return onesie.Request{
			Model: model, State: map[string]any{"photo": "image-a"}, Questions: questions,
			Assets: onesie.Assets{"image-a": square},
		}
	}
	text := onesie.Request{State: "s", Questions: oneNoul()}
	repeated := onesie.Request{
		State:     strings.Repeat("img-a ", 1000),
		Questions: oneNoul(),
		Assets: onesie.Assets{
			"img-a": onesie.Image{Format: "png", Data: bytes.Repeat([]byte{1}, onesie.MaxBodyBytes/1200)},
		},
	}
	tiny := onesie.Request{
		State: map[string]any{"photo": "img"}, Questions: oneNoul(),
		Assets: onesie.Assets{"img": onesie.Image{Format: "png", Data: []byte{1}}},
	}

	refusals := []struct {
		name     string
		provider onesie.Provider
		req      onesie.Request
		wantErr  string
	}{
		{
			name: "should refuse a box question on cloudflare with the error Refuse gives", provider: onesie.Cloudflare(),
			req:     photo("clef-flash", box),
			wantErr: "onesie: the cloudflare provider takes no box questions. The typesafe and openrouter providers do",
		},
		{
			name: "should refuse a valid image on levanto", provider: onesie.Levanto(), req: photo("", oneNoul()),
			wantErr: "onesie: the levanto provider takes no images. The typesafe, openrouter and cloudflare providers do",
		},
		{
			name: "should refuse long context on cloudflare", provider: onesie.Cloudflare(),
			req:     onesie.Request{State: "s", Questions: oneNoul(), LongContext: true},
			wantErr: "onesie: the cloudflare provider takes no long context. The typesafe, openrouter, berget and levanto providers do",
		},
		{
			name: "should refuse a body openrouter would inline past the limit", provider: onesie.OpenRouter(),
			req: repeated, wantErr: "name each image once",
		},
		{
			name:     "should refuse a box question with no assets on cloudflare as the provider, not as invalid",
			provider: onesie.Cloudflare(), req: onesie.Request{State: "s", Questions: box},
			wantErr: "onesie: the cloudflare provider takes no box questions. The typesafe and openrouter providers do",
		},
	}

	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body, err := tc.provider.Encode(tc.req)
			if !errors.Is(err, onesie.ErrValidation) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}

			if body != nil {
				t.Errorf("body = %.100s, want none", body)
			}
		})
	}

	sent := []struct {
		name     string
		provider onesie.Provider
		req      onesie.Request
	}{
		{name: "should encode text as typesafe sends it", provider: onesie.TypeSafe(), req: text},
		{name: "should encode text as openrouter sends it", provider: onesie.OpenRouter(), req: text},
		{name: "should encode text as berget sends it", provider: onesie.Berget(), req: text},
		{name: "should encode text as levanto sends it", provider: onesie.Levanto(), req: text},
		{name: "should encode text as cloudflare sends it to jev", provider: onesie.Cloudflare(), req: text},
		{name: "should encode an image as typesafe sends it", provider: onesie.TypeSafe(), req: photo("", oneNoul())},
		{name: "should encode an image as openrouter sends it", provider: onesie.OpenRouter(), req: photo("", oneNoul())},
		{name: "should encode an image as cloudflare sends it to jev", provider: onesie.Cloudflare(), req: photo("", oneNoul())},
		{
			name: "should encode an image as cloudflare sends it to clef", provider: onesie.Cloudflare(),
			req: photo("clef-flash", oneNoul()),
		},
	}

	for _, tc := range sent {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			transport := &recordingTransport{body: shortAnswer}
			client, _ := newTestClient(t, "https://api.example",
				onesie.WithProvider(tc.provider), onesie.WithHTTPClient(&http.Client{Transport: transport}))

			// The answer is not every provider's shape, so only the body sent matters here.
			_, sendErr := client.SystemOne(t.Context(), tc.req)
			if transport.lastBody() == "" {
				t.Fatalf("SystemOne sent nothing: %v", sendErr)
			}

			body, err := tc.provider.Encode(tc.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := transport.lastBody(); string(body) != got {
				t.Errorf("Encode = %.300s\nSystemOne sent %.300s", body, got)
			}
		})
	}

	t.Run("should write the built in default for a blank model", func(t *testing.T) {
		t.Parallel()

		typesafe, err := onesie.TypeSafe().Encode(text)
		if err != nil || !strings.Contains(string(typesafe), `"model":"`+onesie.TypeSafe().DefaultModel()+`"`) {
			t.Errorf("typesafe = %s, %v, want the default model", typesafe, err)
		}

		cloudflare, err := onesie.Cloudflare().Encode(text)
		if err != nil || !strings.Contains(string(cloudflare), `"model":"`+onesie.Cloudflare().DefaultModel()+`"`) ||
			!strings.Contains(string(cloudflare), `"input":`) {
			t.Errorf("cloudflare = %s, %v, want jev's envelope", cloudflare, err)
		}
	})

	t.Run("should trim the model", func(t *testing.T) {
		t.Parallel()

		body, err := onesie.TypeSafe().Encode(onesie.Request{Model: "  jev-1 ", State: "s", Questions: oneNoul()})
		if err != nil || !strings.Contains(string(body), `"model":"jev-1"`) {
			t.Errorf("body = %s, %v, want the trimmed model", body, err)
		}
	})

	t.Run("should never write long context in the body", func(t *testing.T) {
		t.Parallel()

		body, err := onesie.TypeSafe().Encode(onesie.Request{State: "s", Questions: oneNoul(), LongContext: true})
		if err != nil || strings.Contains(string(body), "long_context") {
			t.Errorf("body = %s, %v, want no long_context", body, err)
		}
	})

	t.Run("should encode a box question with no assets on typesafe, which ValidateRequest rejects", func(t *testing.T) {
		t.Parallel()

		req := onesie.Request{State: "s", Questions: box}
		if onesie.ValidateRequest(req) == nil {
			t.Fatal("ValidateRequest took the request, so this case proves nothing")
		}

		if _, err := onesie.TypeSafe().Encode(req); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("should inline a one byte image on openrouter, which ValidateRequest rejects", func(t *testing.T) {
		t.Parallel()

		if onesie.ValidateRequest(tiny) == nil {
			t.Fatal("ValidateRequest took the request, so this case proves nothing")
		}

		body, err := onesie.OpenRouter().Encode(tiny)
		if err != nil || !strings.Contains(string(body), "data:image/png;base64,AQ==") {
			t.Errorf("body = %s, %v, want the image inlined", body, err)
		}
	})
}

func TestProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		provider        onesie.Provider
		wantName        string
		envAPIKey       string
		envBaseURL      string
		envDefaultModel string
		envAccountID    string
		defaultModel    string
		defaultBaseURL  string
	}{
		{
			name: "should describe typesafe", provider: onesie.TypeSafe(), wantName: "typesafe",
			envAPIKey: "TYPESAFE_API_KEY", envBaseURL: "TYPESAFE_BASE_URL", envDefaultModel: "TYPESAFE_DEFAULT_MODEL",
			defaultModel: "jev-latest", defaultBaseURL: "https://api.typesafe.ai",
		},
		{
			name: "should describe openrouter", provider: onesie.OpenRouter(), wantName: "openrouter",
			envAPIKey: "OPENROUTER_API_KEY", defaultModel: "jev-latest", defaultBaseURL: "https://openrouter.ai/api",
		},
		{
			name: "should describe berget", provider: onesie.Berget(), wantName: "berget",
			envAPIKey: "BERGET_API_KEY", defaultModel: "systemone", defaultBaseURL: "https://api.berget.ai",
		},
		{
			name: "should describe levanto", provider: onesie.Levanto(), wantName: "levanto",
			envAPIKey: "LEVANTO_API_KEY", defaultModel: "sage-latest", defaultBaseURL: "https://sage.levanto.ai",
		},
		{
			name: "should describe cloudflare, whose base URL depends on the account", provider: onesie.Cloudflare(),
			wantName: "cloudflare", envAPIKey: "CLOUDFLARE_API_TOKEN", envAccountID: "CLOUDFLARE_ACCOUNT_ID",
			defaultModel: "typesafe/jev",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := tc.provider
			got := []string{p.Name(), p.EnvAPIKey(), p.EnvBaseURL(), p.EnvDefaultModel(), p.EnvAccountID(), p.DefaultModel(), p.DefaultBaseURL()}
			want := []string{tc.wantName, tc.envAPIKey, tc.envBaseURL, tc.envDefaultModel, tc.envAccountID, tc.defaultModel, tc.defaultBaseURL}

			if strings.Join(got, "|") != strings.Join(want, "|") {
				t.Errorf("accessors = %q, want %q", got, want)
			}
		})
	}

	t.Run("should refuse a zero provider alike everywhere", func(t *testing.T) {
		t.Parallel()

		const want = "onesie: the provider is empty. Use ProviderNamed with typesafe, openrouter, berget, levanto or cloudflare"

		var zero onesie.Provider

		refuseErr := zero.Refuse("", onesie.Needs{})
		_, encodeErr := zero.Encode(onesie.Request{State: "s", Questions: oneNoul()})
		_, newErr := onesie.New(onesie.WithAPIKey("k"), onesie.WithProvider(zero))

		for _, err := range []error{refuseErr, encodeErr, newErr} {
			if !errors.Is(err, onesie.ErrValidation) || err.Error() != want {
				t.Errorf("error = %v, want %q", err, want)
			}
		}
	})
}

func TestProviderResolveBaseURL(t *testing.T) {
	t.Parallel()

	const account = "0123456789abcdef0123456789abcdef"

	accountURL := "https://api.cloudflare.com/client/v4/accounts/" + account + "/ai"

	tests := []struct {
		name     string
		provider onesie.Provider
		explicit string
		account  string
		env      map[string]string
		want     string
		source   onesie.BaseURLSource
		wantErr  error
	}{
		{name: "should give the default", provider: onesie.TypeSafe(), want: "https://api.typesafe.ai", source: onesie.BaseURLFromDefault},
		{
			name: "should read the environment and trim its slash", provider: onesie.TypeSafe(),
			env: map[string]string{"TYPESAFE_BASE_URL": "https://proxy.example/"}, want: "https://proxy.example", source: onesie.BaseURLFromEnv,
		},
		{
			name: "should let the explicit URL win", provider: onesie.TypeSafe(), explicit: " https://explicit.example ",
			env: map[string]string{"TYPESAFE_BASE_URL": "https://proxy.example"}, want: "https://explicit.example", source: onesie.BaseURLFromOption,
		},
		{name: "should build the URL from the account", provider: onesie.Cloudflare(), account: account, want: accountURL, source: onesie.BaseURLFromAccount},
		{
			name: "should build the URL from the account variable", provider: onesie.Cloudflare(),
			env: map[string]string{"CLOUDFLARE_ACCOUNT_ID": account}, want: accountURL, source: onesie.BaseURLFromAccount,
		},
		{
			name: "should take plain http, which only New refuses", provider: onesie.TypeSafe(), explicit: "http://proxy.example",
			want: "http://proxy.example", source: onesie.BaseURLFromOption,
		},
		{name: "should refuse a missing account", provider: onesie.Cloudflare(), wantErr: onesie.ErrNoAccountID},
		{name: "should refuse a zero provider", wantErr: onesie.ErrValidation},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, source, err := tc.provider.ResolveBaseURL(tc.explicit, tc.account, lookupFrom(tc.env))
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}

				return
			}

			if err != nil || got != tc.want || source != tc.source {
				t.Errorf("ResolveBaseURL = %q, %v, %v, want %q from %v", got, source, err, tc.want, tc.source)
			}
		})
	}

	for _, tc := range []struct {
		name     string
		explicit string
		env      map[string]string
		source   onesie.BaseURLSource
	}{
		{name: "should name an explicit URL as the source of its refusal", explicit: "https://k@host", source: onesie.BaseURLFromOption},
		{name: "should name the variable as the source of its refusal", env: map[string]string{"TYPESAFE_BASE_URL": "https://k@host"}, source: onesie.BaseURLFromEnv},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := onesie.TypeSafe().ResolveBaseURL(tc.explicit, "", lookupFrom(tc.env))

			var refused *onesie.BaseURLError
			if !errors.As(err, &refused) || refused.Source != tc.source {
				t.Fatalf("error = %v, want a BaseURLError from %v", err, tc.source)
			}
		})
	}
}
