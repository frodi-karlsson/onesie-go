package onesie_test

import (
	"bytes"
	"errors"
	"net/http"
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

			if provider.Name != tc.want {
				t.Errorf("Name = %q, want %q", provider.Name, tc.want)
			}
		})
	}

	t.Run("should reject an unknown name and list the valid ones", func(t *testing.T) {
		t.Parallel()

		_, err := onesie.ProviderNamed("nope")
		if !errors.Is(err, onesie.ErrValidation) {
			t.Fatalf("error got %v, want ErrValidation", err)
		}

		for _, want := range []string{"nope", "typesafe", "openrouter", "berget", "levanto", "cloudflare"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to name %q", err.Error(), want)
			}
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
		{name: "should take everything on a provider a caller built", provider: onesie.Provider{Name: "mine"}, needs: everything},
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

func TestProviders(t *testing.T) {
	t.Parallel()

	t.Run("should list every provider in a fixed order", func(t *testing.T) {
		t.Parallel()

		var names []string
		for _, provider := range onesie.Providers() {
			names = append(names, provider.Name)
		}

		if got := strings.Join(names, " "); got != "typesafe openrouter berget levanto cloudflare" {
			t.Errorf("providers = %s", got)
		}
	})

	t.Run("should list providers ProviderNamed resolves", func(t *testing.T) {
		t.Parallel()

		for _, provider := range onesie.Providers() {
			named, err := onesie.ProviderNamed(provider.Name)
			if err != nil || named.Name != provider.Name {
				t.Errorf("ProviderNamed(%s) = %v, %v", provider.Name, named.Name, err)
			}
		}
	})
}

func TestCapabilityError(t *testing.T) {
	t.Parallel()

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
			refused.Model != onesie.CloudflareDefaultModel {
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
		if !errors.As(err, &refused) || refused.Model != onesie.LevantoDefaultModel || !refused.Missing.Box {
			t.Errorf("error = %v, want a CapabilityError for the box", err)
		}
	})
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
		if err != nil || !strings.Contains(string(typesafe), `"model":"`+onesie.DefaultModel+`"`) {
			t.Errorf("typesafe = %s, %v, want the default model", typesafe, err)
		}

		cloudflare, err := onesie.Cloudflare().Encode(text)
		if err != nil || !strings.Contains(string(cloudflare), `"model":"`+onesie.CloudflareDefaultModel+`"`) ||
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

	t.Run("should return the marshalled body on a provider built outside the package", func(t *testing.T) {
		t.Parallel()

		body, err := onesie.Provider{Name: "mine"}.Encode(text)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want, err := onesie.MarshalBody(text)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if string(body) != string(want) {
			t.Errorf("body = %s, want %s", body, want)
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
