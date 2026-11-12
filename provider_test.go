package onesie_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
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
		{name: "should return vercel for vercel", input: "vercel", want: "vercel"},
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

	t.Run("should refuse an unknown name and list the valid ones", func(t *testing.T) {
		t.Parallel()

		_, err := onesie.ProviderNamed("nope")
		if !errors.Is(err, onesie.ErrValidation) {
			t.Fatalf("error got %v, want ErrValidation", err)
		}

		const want = "onesie: unknown provider 'nope'. Use typesafe, openrouter, berget, levanto, cloudflare or vercel"
		if err.Error() != want {
			t.Errorf("error = %q, want %q", err.Error(), want)
		}
	})
}

func TestProviderRefuse(t *testing.T) {
	t.Parallel()

	const (
		noImages = "onesie: the levanto provider takes no images. The typesafe, openrouter, cloudflare and vercel providers do"
		noBox    = "onesie: the levanto provider takes no box questions. The typesafe, openrouter and vercel providers do"
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
		{name: "should take everything on vercel", provider: onesie.Vercel(), needs: everything},
		{name: "should refuse images on levanto", provider: onesie.Levanto(), needs: onesie.Needs{Images: true}, wantErr: noImages},
		{
			name: "should refuse images on berget", provider: onesie.Berget(), needs: onesie.Needs{Images: true, Box: true},
			wantErr: "onesie: the berget provider takes no images or box questions. The typesafe, openrouter and vercel providers do",
		},
		{name: "should take long context on levanto", provider: onesie.Levanto(), needs: onesie.Needs{LongContext: true}},
		{name: "should read a blank model as the default", provider: onesie.Levanto(), model: "  ", needs: onesie.Needs{Box: true}, wantErr: noBox},
		{name: "should take images on cloudflare", provider: onesie.Cloudflare(), needs: onesie.Needs{Images: true}},
		{
			name: "should refuse a box question on cloudflare", provider: onesie.Cloudflare(), model: "clef-flash",
			needs:   onesie.Needs{Images: true, Box: true},
			wantErr: "onesie: the cloudflare provider takes no box questions. The typesafe, openrouter and vercel providers do",
		},
		{
			name: "should refuse long context on cloudflare", provider: onesie.Cloudflare(),
			needs:   onesie.Needs{LongContext: true},
			wantErr: "onesie: the cloudflare provider takes no long context. The typesafe, openrouter, berget, levanto and vercel providers do",
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

func TestProviderAlwaysLongContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider onesie.Provider
		model    string
		want     bool
	}{
		{name: "should read a long state in full on openrouter", provider: onesie.OpenRouter(), want: true},
		{name: "should read past the limit only when asked on typesafe", provider: onesie.TypeSafe()},
		{name: "should read past the limit only when asked on berget", provider: onesie.Berget()},
		{name: "should read past the limit only when asked on levanto", provider: onesie.Levanto()},
		{name: "should read past the limit only when asked on vercel", provider: onesie.Vercel()},
		{name: "should keep the usual limit on cloudflare", provider: onesie.Cloudflare()},
		{name: "should keep the usual limit on a clef model", provider: onesie.Cloudflare(), model: "clef-flash"},
		{name: "should answer false for the zero provider", provider: onesie.Provider{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.provider.AlwaysLongContext(tc.model); got != tc.want {
				t.Errorf("AlwaysLongContext(%q) = %v, want %v", tc.model, got, tc.want)
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
		{name: "should not pin a versioned name on vercel, where the gateway chooses what serves it", provider: onesie.Vercel(), model: "jev-1.14.0"},
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

func TestProviderCustomBaseURL(t *testing.T) {
	t.Parallel()

	const account = "0123456789abcdef0123456789abcdef"

	accountURL := "https://api.cloudflare.com/client/v4/accounts/" + account + "/ai"

	tests := []struct {
		name     string
		provider onesie.Provider
		baseURL  string
		want     bool
	}{
		{name: "should treat the default as not custom on typesafe", provider: onesie.TypeSafe(), baseURL: "https://api.typesafe.ai"},
		{name: "should treat the default as not custom on openrouter", provider: onesie.OpenRouter(), baseURL: "https://openrouter.ai/api"},
		{name: "should treat the default as not custom on berget", provider: onesie.Berget(), baseURL: "https://api.berget.ai"},
		{name: "should treat the default as not custom on levanto", provider: onesie.Levanto(), baseURL: "https://sage.levanto.ai"},
		{name: "should treat the default as not custom on vercel", provider: onesie.Vercel(), baseURL: "https://ai-gateway.vercel.sh/typesafe"},
		{name: "should ignore a trailing slash", provider: onesie.TypeSafe(), baseURL: "https://api.typesafe.ai//"},
		{name: "should treat another host as custom", provider: onesie.TypeSafe(), baseURL: "http://localhost:8080", want: true},
		{name: "should treat another provider's default as custom", provider: onesie.TypeSafe(), baseURL: "https://openrouter.ai/api", want: true},
		{name: "should treat a different scheme as custom", provider: onesie.TypeSafe(), baseURL: "http://api.typesafe.ai", want: true},
		{name: "should treat a different case as custom", provider: onesie.TypeSafe(), baseURL: "HTTPS://API.TYPESAFE.AI", want: true},
		{name: "should treat a cloudflare account URL as not custom", provider: onesie.Cloudflare(), baseURL: accountURL},
		{name: "should treat a cloudflare account URL with a trailing slash as not custom", provider: onesie.Cloudflare(), baseURL: accountURL + "/"},
		{name: "should treat a cloudflare proxy URL as custom", provider: onesie.Cloudflare(), baseURL: "https://gateway.example/ai", want: true},
		{name: "should treat a local server as custom on cloudflare", provider: onesie.Cloudflare(), baseURL: "http://localhost:8080", want: true},
		{
			name: "should treat a cloudflare account URL with a malformed account as custom", provider: onesie.Cloudflare(),
			baseURL: "https://api.cloudflare.com/client/v4/accounts/not-an-account/ai", want: true,
		},
		{
			name: "should treat a cloudflare account URL with a short account as custom", provider: onesie.Cloudflare(),
			baseURL: "https://api.cloudflare.com/client/v4/accounts/0123456789abcdef/ai", want: true,
		},
		{
			name: "should treat a cloudflare account URL with a longer path as custom", provider: onesie.Cloudflare(),
			baseURL: accountURL + "/extra", want: true,
		},
		{
			name: "should treat an upper case cloudflare account URL as not custom", provider: onesie.Cloudflare(),
			baseURL: "https://api.cloudflare.com/client/v4/accounts/0123456789ABCDEF0123456789ABCDEF/ai",
		},
		{name: "should treat a padded default as not custom", provider: onesie.TypeSafe(), baseURL: "  https://api.typesafe.ai/ "},
		{name: "should treat the default with a port as custom", provider: onesie.TypeSafe(), baseURL: "https://api.typesafe.ai:443", want: true},
		{name: "should treat a blank URL as not custom", provider: onesie.TypeSafe(), baseURL: ""},
		{name: "should treat a whitespace URL as not custom", provider: onesie.TypeSafe(), baseURL: "  "},
		{name: "should treat a blank URL as not custom on cloudflare", provider: onesie.Cloudflare(), baseURL: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.provider.CustomBaseURL(tc.baseURL); got != tc.want {
				t.Errorf("%s CustomBaseURL(%q) = %v, want %v", tc.provider.Name(), tc.baseURL, got, tc.want)
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
		{name: "should name jev-latest on vercel, which serves no jev-preview", provider: onesie.Vercel(), want: "jev-latest"},
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
		{name: "should write the result as it is on vercel when it has no cost", provider: onesie.Vercel(), want: answered},
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

	t.Run("should write a cost where the gateway sends it on vercel, so it round trips", func(t *testing.T) {
		t.Parallel()

		cost := 0.000016674
		costed := &onesie.Result{Model: "m", Answers: result.Answers, Usage: onesie.Usage{InputTokens: 3, Cost: &cost}}

		body, err := onesie.Vercel().ResponseBody("", costed)
		want := `{"answers":{"q":{"type":"noul","noul":0.1}},"model":"m","provider_metadata":{"gateway":{"cost":"0.000016674"}},` +
			`"usage":{"input_tokens":3,"output_tokens":0}}`

		if err != nil || string(body) != want {
			t.Fatalf("ResponseBody = %s, %v, want %s", body, err, want)
		}

		transport := &recordingTransport{body: string(body)}
		client, _ := newTestClient(t, "https://api.example", onesie.WithProvider(onesie.Vercel()),
			onesie.WithHTTPClient(&http.Client{Transport: transport}))

		got, err := client.SystemOne(t.Context(), onesie.Request{State: "s", Questions: oneNoul()})
		if err != nil || got.Usage.Cost == nil || *got.Usage.Cost != cost {
			t.Errorf("result = %+v, %v, want the cost %v back", got, err, cost)
		}
	})

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

		if got := strings.Join(names, " "); got != "typesafe openrouter berget levanto cloudflare vercel" {
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
			strings.Join(refused.Takers, " ") != "typesafe openrouter vercel" {
			t.Errorf("error = %+v", refused)
		}

		const want = "onesie: the cloudflare provider takes no box questions or long context. " +
			"The typesafe, openrouter and vercel providers do"
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

	t.Run("should be a ValidationError as well", func(t *testing.T) {
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

func TestProviderPrepare(t *testing.T) {
	// Before t.Parallel, so the allocation count runs while no other test does, since it reads the
	// whole process.
	t.Run("should allocate about one request body per route stage for an image", func(t *testing.T) {
		file := noisePNG(t, 600)

		fixed, err := onesie.NewImage(file)
		if err != nil {
			t.Fatal(err)
		}

		questions := onesie.Questions{{ID: "red", Question: onesie.Noul{Instructions: "is there red in the photo"}}}

		// Bodies allocated per prepare, a little above what each route needs, so one more copy of the
		// image fails. A per-record image adds its encoded asset, one body more.
		for _, tc := range []struct {
			name     string
			provider onesie.Provider
			model    string
			bodies   float64
		}{
			{name: "typesafe", provider: onesie.TypeSafe(), bodies: 1.5},
			{name: "openrouter", provider: onesie.OpenRouter(), bodies: 1.5},
			{name: "cloudflare", provider: onesie.Cloudflare(), bodies: 4},
			{name: "cloudflare clef", provider: onesie.Cloudflare(), model: "clef-flash", bodies: 6.3},
		} {
			for _, perRecord := range []bool{false, true} {
				prepare := func() *onesie.PreparedBody {
					photo := fixed
					if perRecord {
						if photo, err = onesie.NewImage(file); err != nil {
							t.Fatal(err)
						}
					}

					prepared, prepareErr := tc.provider.Prepare(onesie.Request{
						State: map[string]any{"photo": "img-1"}, Model: tc.model, Questions: questions,
						Assets: onesie.Assets{"img-1": photo},
					})
					if prepareErr != nil {
						t.Fatal(prepareErr)
					}

					return prepared
				}

				body := float64(len(prepare().Bytes()))
				limit := tc.bodies
				if perRecord {
					limit++
				}

				const runs = 5

				var before, after runtime.MemStats

				runtime.ReadMemStats(&before)

				for range runs {
					prepare()
				}

				runtime.ReadMemStats(&after)

				if got := float64(after.TotalAlloc-before.TotalAlloc) / runs / body; got > limit {
					t.Errorf("%s, per-record %v: %.2f bodies allocated per prepare, want at most %.1f", tc.name, perRecord, got, limit)
				}
			}
		}
	})

	t.Parallel()

	square, err := onesie.NewImage(squarePNG(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	box := onesie.Questions{{ID: "mug", Question: onesie.Box{Instructions: "the mug in image-a"}}}
	photo := onesie.Request{State: "image-a", Questions: box, Assets: onesie.Assets{"image-a": square}}
	noBoxes := "onesie: the cloudflare provider takes no box questions. The typesafe, openrouter and vercel providers do"

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
			name: "should refuse a box question with no image", provider: onesie.TypeSafe(),
			req:     onesie.Request{State: "s", Questions: box},
			wantErr: "onesie: box question 'mug' needs an image in the request",
		},
		{
			name: "should refuse what the provider's route does not take", provider: onesie.Levanto(),
			req:     onesie.Request{State: "image-a", Questions: oneNoul(), Assets: onesie.Assets{"image-a": square}},
			wantErr: "onesie: the levanto provider takes no images. The typesafe, openrouter, cloudflare and vercel providers do",
		},
		{name: "should refuse a box question on a route that takes none", provider: onesie.Cloudflare(), req: photo, wantErr: noBoxes},
		{
			name: "should refuse a pointer to a box question as it refuses a box question", provider: onesie.Cloudflare(),
			req: onesie.Request{
				State: "image-a", Assets: photo.Assets,
				Questions: onesie.Questions{{ID: "mug", Question: &onesie.Box{Instructions: "the mug in image-a"}}},
			},
			wantErr: noBoxes,
		},
		{
			name: "should refuse a blank model as the built in default refuses it", provider: onesie.Cloudflare(),
			req:     onesie.Request{Model: " ", State: "s", Questions: oneNoul(), LongContext: true},
			wantErr: "onesie: the cloudflare provider takes no long context. The typesafe, openrouter, berget, levanto and vercel providers do",
		},
		{
			name: "should refuse long context from WithLongContext as SystemOne does", provider: onesie.Cloudflare(),
			req: onesie.Request{State: "s", Questions: oneNoul()}, opts: []onesie.RequestOption{onesie.WithLongContext()},
			wantErr: "onesie: the cloudflare provider takes no long context. The typesafe, openrouter, berget, levanto and vercel providers do",
		},
		{
			name: "should take WithLongContext on a provider that takes long context", provider: onesie.TypeSafe(),
			req: onesie.Request{State: "s", Questions: oneNoul()}, opts: []onesie.RequestOption{onesie.WithLongContext()},
		},
		{
			name: "should refuse a failing option", provider: onesie.TypeSafe(), req: onesie.Request{State: "s", Questions: oneNoul()},
			opts: []onesie.RequestOption{onesie.WithRequestAttemptTimeout(0)}, wantErr: "onesie: attempt timeout must be positive",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			prepared, err := tc.provider.Prepare(tc.req, tc.opts...)
			if tc.wantErr == "" {
				if err != nil || prepared == nil {
					t.Fatalf("Prepare = %v, %v, want a body", prepared, err)
				}

				return
			}

			if !errors.Is(err, onesie.ErrValidation) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}

	t.Run("should report the body MarshalBody writes before any route rewrites it, with its model and long context", func(t *testing.T) {
		t.Parallel()

		var checked int

		for _, provider := range []onesie.Provider{onesie.TypeSafe(), onesie.Cloudflare()} {
			for _, tc := range encodeCorpus(t) {
				for _, longContext := range []bool{false, true} {
					var opts []onesie.RequestOption
					if longContext {
						opts = append(opts, onesie.WithLongContext())
					}

					prepared, err := provider.Prepare(tc.req, opts...)
					if err != nil {
						continue
					}

					resolved := tc.req
					resolved.Model = provider.DefaultModel()
					resolved.LongContext = tc.req.LongContext || longContext

					want, err := onesie.MarshalBody(resolved)
					if err != nil || !bytes.Equal(prepared.Bytes(), want) ||
						prepared.Model() != resolved.Model || prepared.LongContext() != resolved.LongContext {
						t.Errorf("%s %s with long context %v: Bytes = %s, Model = %q, LongContext = %v, want %s, %q and %v",
							provider.Name(), tc.name, longContext, prepared.Bytes(), prepared.Model(), prepared.LongContext(),
							want, resolved.Model, resolved.LongContext)
					}

					checked++
				}
			}
		}

		if checked == 0 {
			t.Fatal("no corpus case prepared")
		}
	})

	t.Run("should trim the model it is given", func(t *testing.T) {
		t.Parallel()

		prepared, err := onesie.TypeSafe().Prepare(onesie.Request{Model: "  jev-1 ", State: "s", Questions: oneNoul()})
		if err != nil || prepared.Model() != "jev-1" || !strings.Contains(string(prepared.Bytes()), `"model":"jev-1"`) {
			t.Errorf("Prepare = %v, %v, want the trimmed model", prepared, err)
		}
	})

	t.Run("should check a result against its questions as prepared, whatever the caller changes afterwards", func(t *testing.T) {
		t.Parallel()

		questions := onesie.Questions{
			{ID: "pick", Question: onesie.Choice{Instructions: "Which?", Criteria: onesie.Criteria{{Name: "a", Desc: "A"}, {Name: "b", Desc: "B"}}}},
			{ID: "rate", Question: &onesie.Score{Instructions: "How?", Criteria: []any{"low", "high"}}},
		}

		prepared, err := onesie.TypeSafe().Prepare(onesie.Request{State: "s", Questions: questions})
		if err != nil {
			t.Fatal(err)
		}

		questions[0].ID = "changed"
		questions[0].Question.(onesie.Choice).Criteria[0].Name = "z"
		questions[1].Question.(*onesie.Score).Criteria = []any{"only"}

		var result onesie.Result

		answers := `{"answers":{"pick":{"type":"choice","choice":"a","probabilities":{"a":0.8,"b":0.2}},"rate":{"type":"score","score":1,"probabilities":{"0":0,"1":1}}}}`
		if err := json.Unmarshal([]byte(answers), &result); err != nil {
			t.Fatal(err)
		}

		if err := prepared.CheckResult(&result); err != nil {
			t.Errorf("CheckResult = %v, want the answers to fit the questions as prepared", err)
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
		{
			name: "should describe vercel", provider: onesie.Vercel(), wantName: "vercel",
			envAPIKey: "AI_GATEWAY_API_KEY", defaultModel: "jev-latest", defaultBaseURL: "https://ai-gateway.vercel.sh/typesafe",
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

		const want = "onesie: the provider is empty. Use ProviderNamed with typesafe, openrouter, berget, levanto, cloudflare or vercel"

		var zero onesie.Provider

		refuseErr := zero.Refuse("", onesie.Needs{})
		_, prepareErr := zero.Prepare(onesie.Request{State: "s", Questions: oneNoul()})
		_, newErr := onesie.New(onesie.WithAPIKey("k"), onesie.WithProvider(zero))

		for _, err := range []error{refuseErr, prepareErr, newErr} {
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
