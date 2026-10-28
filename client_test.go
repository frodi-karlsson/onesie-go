package onesie_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frodi-karlsson/onesie-go"
)

const shortAnswer = `{"model":"m","answers":{"q":{"type":"noul","noul":0.1}},"usage":{}}`

func ExampleClient_SystemOne() {
	client, err := onesie.New(onesie.WithAPIKey("key"))
	if err != nil {
		panic(err)
	}

	_, _ = client.SystemOne(context.Background(), onesie.Request{
		State: "Please restore service today.",
		Questions: onesie.Questions{{
			ID: "urgent", Question: onesie.Noul{Instructions: "Is this urgent?"},
		}},
	})
}

func TestClientSystemOneURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options []onesie.Option
		want    string
	}{
		{
			name: "should add the API path to each provider's default base URL",
			want: onesie.TypeSafe().DefaultBaseURL() + "/v1/systemone",
		},
		{
			name:    "should add the API path to a base URL whose path already ends in /v1",
			options: []onesie.Option{onesie.WithBaseURL("https://proxy.example/v1/")},
			want:    "https://proxy.example/v1/v1/systemone",
		},
		{
			name:    "should add the API path to openrouter's base URL",
			options: []onesie.Option{onesie.WithProvider(onesie.OpenRouter())},
			want:    "https://openrouter.ai/api/v1/systemone",
		},
		{
			name:    "should add the API path to berget's base URL",
			options: []onesie.Option{onesie.WithProvider(onesie.Berget())},
			want:    "https://api.berget.ai/v1/systemone",
		},
		{
			name:    "should add the API path to levanto's base URL",
			options: []onesie.Option{onesie.WithProvider(onesie.Levanto())},
			want:    "https://sage.levanto.ai/v1/systemone",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			options := append([]onesie.Option{
				onesie.WithAPIKey("k"), onesie.WithEnv(func(string) (string, bool) { return "", false }),
			}, tc.options...)

			client, err := onesie.New(options...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := client.SystemOneURL(); got != tc.want {
				t.Errorf("SystemOneURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClientBaseURLRepeatsPath(t *testing.T) {
	t.Parallel()

	const account = "0123456789abcdef0123456789abcdef"

	tests := []struct {
		name        string
		options     []onesie.Option
		wantSegment string
		wantRepeats bool
	}{
		{name: "should find no repeat in the default base URL"},
		{
			name:        "should find v1 at the end of a typesafe base URL",
			options:     []onesie.Option{onesie.WithBaseURL("https://proxy.example/v1/")},
			wantSegment: "v1", wantRepeats: true,
		},
		{
			name:        "should find v1 at the end of an openrouter base URL",
			options:     []onesie.Option{onesie.WithProvider(onesie.OpenRouter()), onesie.WithBaseURL("https://openrouter.ai/api/v1")},
			wantSegment: "v1", wantRepeats: true,
		},
		{
			name:    "should find no repeat in a v1 that is only part of the last segment",
			options: []onesie.Option{onesie.WithBaseURL("https://proxy.example/apiv1")},
		},
		{
			name: "should find no repeat in a cloudflare base URL ending in v1, since cloudflare posts to run",
			options: []onesie.Option{
				onesie.WithProvider(onesie.Cloudflare()), onesie.WithBaseURL("https://api.cloudflare.com/client/v4/accounts/" + account + "/ai/v1"),
			},
		},
		{
			name: "should find run at the end of a cloudflare base URL",
			options: []onesie.Option{
				onesie.WithProvider(onesie.Cloudflare()), onesie.WithBaseURL("https://api.cloudflare.com/client/v4/accounts/" + account + "/ai/run"),
			},
			wantSegment: "run", wantRepeats: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			options := append([]onesie.Option{
				onesie.WithAPIKey("k"), onesie.WithEnv(func(string) (string, bool) { return "", false }),
			}, tc.options...)

			client, err := onesie.New(options...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if segment, repeats := client.BaseURLRepeatsPath(); segment != tc.wantSegment || repeats != tc.wantRepeats {
				t.Errorf("BaseURLRepeatsPath() = %q, %v, want %q, %v", segment, repeats, tc.wantSegment, tc.wantRepeats)
			}
		})
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("should find the cloudflare account", func(t *testing.T) {
		t.Parallel()

		const account = "0123456789abcdef0123456789abcdef"

		tests := []struct {
			name    string
			env     map[string]string
			opts    []onesie.Option
			wantURL string
			wantErr string
		}{
			{
				name:    "should build the base url from CLOUDFLARE_ACCOUNT_ID",
				env:     map[string]string{"CLOUDFLARE_API_TOKEN": "t", "CLOUDFLARE_ACCOUNT_ID": " " + account + " "},
				wantURL: "https://api.cloudflare.com/client/v4/accounts/" + account + "/ai/run",
			},
			{
				name: "should refuse a missing account",
				env:  map[string]string{"CLOUDFLARE_API_TOKEN": "t"},
				wantErr: "onesie: no cloudflare account id. Set CLOUDFLARE_ACCOUNT_ID, pass WithAccountID, or pass the base URL " +
					"https://api.cloudflare.com/client/v4/accounts/<account_id>/ai",
			},
			{
				name:    "should refuse a malformed account",
				env:     map[string]string{"CLOUDFLARE_API_TOKEN": "t", "CLOUDFLARE_ACCOUNT_ID": "nope"},
				wantErr: "onesie: CLOUDFLARE_ACCOUNT_ID must be 32 hexadecimal characters",
			},
			{
				name:    "should let WithBaseURL win with no account",
				env:     map[string]string{"CLOUDFLARE_API_TOKEN": "t"},
				opts:    []onesie.Option{onesie.WithBaseURL("https://proxy.example/ai")},
				wantURL: "https://proxy.example/ai/run",
			},
			{
				name:    "should let WithBaseURL win over a malformed account",
				env:     map[string]string{"CLOUDFLARE_API_TOKEN": "t", "CLOUDFLARE_ACCOUNT_ID": "nope"},
				opts:    []onesie.Option{onesie.WithBaseURL("https://proxy.example/ai")},
				wantURL: "https://proxy.example/ai/run",
			},
			{
				name: "should report the account before the key",
				env:  map[string]string{},
				wantErr: "onesie: no cloudflare account id. Set CLOUDFLARE_ACCOUNT_ID, pass WithAccountID, or pass the base URL " +
					"https://api.cloudflare.com/client/v4/accounts/<account_id>/ai",
			},
			{
				name:    "should build the base url from WithAccountID",
				env:     map[string]string{"CLOUDFLARE_API_TOKEN": "t"},
				opts:    []onesie.Option{onesie.WithAccountID(account)},
				wantURL: "https://api.cloudflare.com/client/v4/accounts/" + account + "/ai/run",
			},
			{
				name:    "should let WithAccountID win over CLOUDFLARE_ACCOUNT_ID",
				env:     map[string]string{"CLOUDFLARE_API_TOKEN": "t", "CLOUDFLARE_ACCOUNT_ID": "nope"},
				opts:    []onesie.Option{onesie.WithAccountID(account)},
				wantURL: "https://api.cloudflare.com/client/v4/accounts/" + account + "/ai/run",
			},
			{
				name:    "should let WithBaseURL win over WithAccountID",
				env:     map[string]string{"CLOUDFLARE_API_TOKEN": "t"},
				opts:    []onesie.Option{onesie.WithAccountID(account), onesie.WithBaseURL("https://proxy.example/ai")},
				wantURL: "https://proxy.example/ai/run",
			},
			{
				name:    "should ignore a blank WithAccountID",
				env:     map[string]string{"CLOUDFLARE_API_TOKEN": "t", "CLOUDFLARE_ACCOUNT_ID": account},
				opts:    []onesie.Option{onesie.WithAccountID("  ")},
				wantURL: "https://api.cloudflare.com/client/v4/accounts/" + account + "/ai/run",
			},
			{
				name:    "should refuse a malformed WithAccountID, naming the account id",
				env:     map[string]string{"CLOUDFLARE_API_TOKEN": "t"},
				opts:    []onesie.Option{onesie.WithAccountID("nope")},
				wantErr: "onesie: the account id must be 32 hexadecimal characters",
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				opts := append([]onesie.Option{onesie.WithEnv(lookupFrom(tc.env)), onesie.WithProvider(onesie.Cloudflare())}, tc.opts...)

				client, err := onesie.New(opts...)
				if tc.wantErr != "" {
					if !errors.Is(err, onesie.ErrValidation) || err.Error() != tc.wantErr {
						t.Fatalf("error = %v, want %q", err, tc.wantErr)
					}

					return
				}

				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if got := client.SystemOneURL(); got != tc.wantURL {
					t.Errorf("SystemOneURL = %q, want %q", got, tc.wantURL)
				}
			})
		}
	})

	t.Run("should resolve the key and the account", func(t *testing.T) {
		t.Parallel()

		const account = "0123456789abcdef0123456789abcdef"

		t.Run("should refuse an account on a provider without accounts", func(t *testing.T) {
			t.Parallel()

			_, err := onesie.New(onesie.WithEnv(lookupFrom(nil)), onesie.WithAPIKey("k"), onesie.WithAccountID(account))
			if !errors.Is(err, onesie.ErrValidation) || err.Error() != "onesie: the typesafe provider takes no account id" {
				t.Fatalf("error = %v, want the account refused", err)
			}
		})

		t.Run("should take the account whichever order the options come in", func(t *testing.T) {
			t.Parallel()

			client, err := onesie.New(onesie.WithEnv(lookupFrom(nil)), onesie.WithAccountID(account),
				onesie.WithAPIKey("k"), onesie.WithProvider(onesie.Cloudflare()))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !strings.Contains(client.BaseURL(), account) {
				t.Errorf("BaseURL = %s, want the account", client.BaseURL())
			}
		})

		t.Run("should match ErrNoAccountID for a missing account", func(t *testing.T) {
			t.Parallel()

			_, err := onesie.New(onesie.WithEnv(lookupFrom(nil)), onesie.WithAPIKey("k"), onesie.WithProvider(onesie.Cloudflare()))
			if !errors.Is(err, onesie.ErrNoAccountID) || !errors.Is(err, onesie.ErrValidation) {
				t.Fatalf("error = %v, want ErrNoAccountID", err)
			}
		})

		t.Run("should call the key func only after the base url passes", func(t *testing.T) {
			t.Parallel()

			calls := 0
			key := func() (string, error) {
				calls++

				return "k", nil
			}

			_, err := onesie.New(onesie.WithEnv(lookupFrom(nil)), onesie.WithAPIKeyFunc(key), onesie.WithBaseURL("not a url"))
			if !errors.Is(err, onesie.ErrValidation) || calls != 0 {
				t.Fatalf("error = %v after %d calls, want a base url refusal and no call", err, calls)
			}
		})

		t.Run("should return the key func's error as it is", func(t *testing.T) {
			t.Parallel()

			locked := errors.New("the keychain is locked")

			_, err := onesie.New(onesie.WithEnv(lookupFrom(nil)), onesie.WithAPIKeyFunc(func() (string, error) { return "", locked }))
			if !errors.Is(err, locked) || err.Error() != "the keychain is locked" {
				t.Fatalf("error = %v, want the func's own error", err)
			}
		})

		t.Run("should refuse a nil key func", func(t *testing.T) {
			t.Parallel()

			_, err := onesie.New(onesie.WithAPIKeyFunc(nil))
			if !errors.Is(err, onesie.ErrValidation) || err.Error() != "onesie: API key func must not be nil" {
				t.Fatalf("error = %v, want the nil func refused", err)
			}
		})

		for _, opt := range []onesie.Option{
			onesie.WithAPIKey(" "),
			onesie.WithAPIKeyFunc(func() (string, error) { return "", nil }),
		} {
			t.Run("should refuse an explicit empty key and not read the environment", func(t *testing.T) {
				t.Parallel()

				_, err := onesie.New(onesie.WithEnv(lookupFrom(map[string]string{"TYPESAFE_API_KEY": "sk-env"})), opt)
				if !errors.Is(err, onesie.ErrNoAPIKey) || err.Error() != "onesie: the API key is empty" {
					t.Fatalf("error = %v, want the empty key refused", err)
				}
			})
		}

		t.Run("should let the last key option win", func(t *testing.T) {
			t.Parallel()

			transport := &recordingTransport{body: shortAnswer}

			client, err := onesie.New(onesie.WithEnv(lookupFrom(nil)),
				onesie.WithAPIKeyFunc(func() (string, error) { return "sk-func", nil }), onesie.WithAPIKey("sk-last"),
				onesie.WithBaseURL("https://api.example"), onesie.WithHTTPClient(&http.Client{Transport: transport}))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if _, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := transport.last().Header.Get("Authorization"); got != "Bearer sk-last" {
				t.Errorf("Authorization = %q, want the last key", got)
			}
		})
	})

	mockEnv := func(values map[string]string) func(string) (string, bool) {
		return func(name string) (string, bool) {
			value, ok := values[name]

			return value, ok
		}
	}

	t.Run("should fail without an api key", func(t *testing.T) {
		t.Parallel()

		_, err := onesie.New(onesie.WithEnv(mockEnv(nil)))
		if err == nil {
			t.Fatalf("expected an error, got none")
		}

		for _, want := range []string{onesie.TypeSafe().EnvAPIKey(), "no API key"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to name %q", err.Error(), want)
			}
		}

		if !errors.Is(err, onesie.ErrNoAPIKey) || err.Error() != "onesie: no API key. Set TYPESAFE_API_KEY" {
			t.Errorf("error = %v, want ErrNoAPIKey naming the variable", err)
		}
	})

	t.Run("should read the api key from the environment", func(t *testing.T) {
		t.Parallel()

		client, err := onesie.New(onesie.WithEnv(mockEnv(map[string]string{onesie.TypeSafe().EnvAPIKey(): "sk-from-env"})))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if client.AttemptTimeout() != onesie.DefaultAttemptTimeout {
			t.Errorf("attempt timeout got %v", client.AttemptTimeout())
		}

		if client.RetryPolicy().MaxRetries != 2 {
			t.Errorf("max retries got %d, want 2", client.RetryPolicy().MaxRetries)
		}
	})

	t.Run("should prefer an explicit option over the environment", func(t *testing.T) {
		t.Parallel()

		var seen atomic.Value

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Model string `json:"model"`
			}

			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			seen.Store(body.Model)

			_, _ = io.WriteString(w, shortAnswer)
		}))
		defer server.Close()

		client, err := onesie.New(
			onesie.WithEnv(mockEnv(map[string]string{
				onesie.TypeSafe().EnvAPIKey():       "sk-from-env",
				onesie.TypeSafe().EnvDefaultModel(): "onesie-from-env",
			})),
			onesie.WithAPIKey("sk-explicit"),
			onesie.WithBaseURL(server.URL),
			onesie.WithDefaultModel("jev-explicit"),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if seen.Load() != "jev-explicit" {
			t.Errorf("model got %v, want %q", seen.Load(), "jev-explicit")
		}
	})

	t.Run("should ignore a blank environment value", func(t *testing.T) {
		t.Parallel()

		_, err := onesie.New(onesie.WithEnv(mockEnv(map[string]string{
			onesie.TypeSafe().EnvAPIKey():  "   ",
			onesie.TypeSafe().EnvBaseURL(): "   ",
		})))

		if err == nil {
			t.Fatalf("a whitespace only key should be rejected")
		}
	})

	t.Run("should reject an invalid base url", func(t *testing.T) {
		t.Parallel()

		_, err := onesie.New(
			onesie.WithEnv(mockEnv(nil)),
			onesie.WithAPIKey("sk"),
			onesie.WithBaseURL("not a url"),
		)

		if !errors.Is(err, onesie.ErrValidation) {
			t.Errorf("error got %v, want ErrValidation", err)
		}
	})

	t.Run("should show the scheme and host of a refused base url", func(t *testing.T) {
		t.Parallel()

		_, err := onesie.New(
			onesie.WithEnv(mockEnv(nil)),
			onesie.WithAPIKey("sk"),
			onesie.WithBaseURL("https:///"),
		)

		if err == nil || err.Error() != "onesie: base URL https:// names no host" {
			t.Errorf("error = %v, want the scheme and the empty host shown", err)
		}
	})

	t.Run("should refuse a plain http base url to another host", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name   string
			opts   []onesie.Option
			env    map[string]string
			source onesie.BaseURLSource
		}{
			{name: "should name an option as the source", opts: []onesie.Option{onesie.WithBaseURL("http://proxy.example")}, source: onesie.BaseURLFromOption},
			{name: "should name the variable as the source", env: map[string]string{"TYPESAFE_BASE_URL": "http://proxy.example"}, source: onesie.BaseURLFromEnv},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				_, err := onesie.New(append([]onesie.Option{onesie.WithEnv(lookupFrom(tc.env)), onesie.WithAPIKey("k")}, tc.opts...)...)

				var refused *onesie.BaseURLError
				if !errors.As(err, &refused) || !errors.Is(err, onesie.ErrInsecureBaseURL) || !errors.Is(err, onesie.ErrValidation) {
					t.Fatalf("error = %v, want an insecure BaseURLError", err)
				}

				const want = "onesie: base URL http://proxy.example is plain http to a host other than this machine"
				if err.Error() != want || refused.Source != tc.source {
					t.Errorf("error = %q from %v, want %q from %v", err.Error(), refused.Source, want, tc.source)
				}
			})
		}
	})

	t.Run("should take a plain http base url with WithInsecureHTTP", func(t *testing.T) {
		t.Parallel()

		client, err := onesie.New(onesie.WithEnv(lookupFrom(nil)), onesie.WithAPIKey("k"),
			onesie.WithBaseURL("http://proxy.example"), onesie.WithInsecureHTTP())
		if err != nil || client.BaseURL() != "http://proxy.example" {
			t.Fatalf("New = %v, %v, want the plain http URL taken", client, err)
		}
	})

	t.Run("should refuse a bad base url before a missing key", func(t *testing.T) {
		t.Parallel()

		_, err := onesie.New(onesie.WithEnv(lookupFrom(nil)), onesie.WithBaseURL("http://proxy.example"))
		if !errors.Is(err, onesie.ErrInsecureBaseURL) || errors.Is(err, onesie.ErrNoAPIKey) {
			t.Fatalf("error = %v, want the base url refused first", err)
		}
	})

	t.Run("should reject an invalid retry policy", func(t *testing.T) {
		t.Parallel()

		policy := onesie.DefaultRetryPolicy()
		policy.MaxRetries = -1

		if _, err := onesie.New(onesie.WithEnv(mockEnv(nil)), onesie.WithAPIKey("sk"), onesie.WithRetry(policy)); err == nil {
			t.Fatalf("expected an error, got none")
		}
	})

	t.Run("should reject a non positive attempt timeout", func(t *testing.T) {
		t.Parallel()

		if _, err := onesie.New(onesie.WithEnv(mockEnv(nil)), onesie.WithAPIKey("sk"), onesie.WithAttemptTimeout(0)); err == nil {
			t.Fatalf("expected an error, got none")
		}
	})

	t.Run("should read OPENROUTER_API_KEY and ignore TYPESAFE_API_KEY under openrouter", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: shortAnswer}

		client, err := onesie.New(
			onesie.WithEnv(mockEnv(map[string]string{
				onesie.TypeSafe().EnvAPIKey(): "sk-typesafe",
				"OPENROUTER_API_KEY":          "sk-or-test",
			})),
			onesie.WithProvider(onesie.OpenRouter()),
			onesie.WithHTTPClient(&http.Client{Transport: transport}),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got := transport.last().Header.Get("Authorization"); got != "Bearer sk-or-test" {
			t.Errorf("Authorization = %q, want the OpenRouter key", got)
		}
	})

	t.Run("should ignore the TypeSafe base url and model env vars under openrouter", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: shortAnswer}

		client, err := onesie.New(
			onesie.WithEnv(mockEnv(map[string]string{
				onesie.TypeSafe().EnvBaseURL():      "https://typesafe.example",
				onesie.TypeSafe().EnvDefaultModel(): "onesie-1.2.0",
			})),
			onesie.WithAPIKey("sk-or-test"),
			onesie.WithProvider(onesie.OpenRouter()),
			onesie.WithHTTPClient(&http.Client{Transport: transport}),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		req := transport.last()
		if got := req.URL.String(); got != "https://openrouter.ai/api/v1/systemone" {
			t.Errorf("url = %s, want the OpenRouter default", got)
		}

		if !strings.Contains(transport.lastBody(), `"model":"`+onesie.TypeSafe().DefaultModel()+`"`) {
			t.Errorf("body = %s, want the default model", transport.lastBody())
		}
	})

	t.Run("should name OPENROUTER_API_KEY in the no key error under openrouter", func(t *testing.T) {
		t.Parallel()

		_, err := onesie.New(onesie.WithEnv(mockEnv(map[string]string{onesie.TypeSafe().EnvAPIKey(): "sk-typesafe"})),
			onesie.WithProvider(onesie.OpenRouter()))
		if err == nil {
			t.Fatalf("expected an error, got none")
		}

		if !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
			t.Errorf("error = %q, want it to name OPENROUTER_API_KEY", err.Error())
		}
	})

	t.Run("should read BERGET_API_KEY and ignore the TypeSafe env vars under berget", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: shortAnswer}

		client, err := onesie.New(
			onesie.WithEnv(mockEnv(map[string]string{
				onesie.TypeSafe().EnvAPIKey():       "sk-typesafe",
				onesie.TypeSafe().EnvBaseURL():      "https://typesafe.example",
				onesie.TypeSafe().EnvDefaultModel(): "onesie-1.2.0",
				"BERGET_API_KEY":                    "sk_ber_test",
			})),
			onesie.WithProvider(onesie.Berget()),
			onesie.WithHTTPClient(&http.Client{Transport: transport}),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		req := transport.last()
		if got := req.Header.Get("Authorization"); got != "Bearer sk_ber_test" {
			t.Errorf("Authorization = %q, want the Berget key", got)
		}

		if got := req.URL.String(); got != "https://api.berget.ai/v1/systemone" {
			t.Errorf("url = %s, want the Berget default", got)
		}

		if !strings.Contains(transport.lastBody(), `"model":"`+onesie.Berget().DefaultModel()+`"`) {
			t.Errorf("body = %s, want the Berget default model", transport.lastBody())
		}
	})

	t.Run("should name BERGET_API_KEY in the no key error under berget", func(t *testing.T) {
		t.Parallel()

		_, err := onesie.New(onesie.WithEnv(mockEnv(map[string]string{onesie.TypeSafe().EnvAPIKey(): "sk-typesafe"})),
			onesie.WithProvider(onesie.Berget()))
		if err == nil {
			t.Fatalf("expected an error, got none")
		}

		if !strings.Contains(err.Error(), "BERGET_API_KEY") {
			t.Errorf("error = %q, want it to name BERGET_API_KEY", err.Error())
		}
	})

	t.Run("should read LEVANTO_API_KEY and ignore the TypeSafe env vars under levanto", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: shortAnswer}

		client, err := onesie.New(
			onesie.WithEnv(mockEnv(map[string]string{
				onesie.TypeSafe().EnvAPIKey():       "sk-typesafe",
				onesie.TypeSafe().EnvBaseURL():      "https://typesafe.example",
				onesie.TypeSafe().EnvDefaultModel(): "onesie-1.2.0",
				"LEVANTO_API_KEY":                   "lv_live_test",
			})),
			onesie.WithProvider(onesie.Levanto()),
			onesie.WithHTTPClient(&http.Client{Transport: transport}),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		req := transport.last()
		if got := req.Header.Get("Authorization"); got != "Bearer lv_live_test" {
			t.Errorf("Authorization = %q, want the Levanto key", got)
		}

		if got := req.URL.String(); got != "https://sage.levanto.ai/v1/systemone" {
			t.Errorf("url = %s, want the Levanto default", got)
		}

		if !strings.Contains(transport.lastBody(), `"model":"`+onesie.Levanto().DefaultModel()+`"`) {
			t.Errorf("body = %s, want the Levanto default model", transport.lastBody())
		}
	})

	t.Run("should name LEVANTO_API_KEY in the no key error under levanto", func(t *testing.T) {
		t.Parallel()

		_, err := onesie.New(onesie.WithEnv(mockEnv(map[string]string{onesie.TypeSafe().EnvAPIKey(): "sk-typesafe"})),
			onesie.WithProvider(onesie.Levanto()))
		if err == nil {
			t.Fatalf("expected an error, got none")
		}

		if !strings.Contains(err.Error(), "LEVANTO_API_KEY") {
			t.Errorf("error = %q, want it to name LEVANTO_API_KEY", err.Error())
		}
	})

	t.Run("should reject a zero provider", func(t *testing.T) {
		t.Parallel()

		_, err := onesie.New(onesie.WithEnv(mockEnv(nil)), onesie.WithAPIKey("sk"), onesie.WithProvider(onesie.Provider{}))
		if !errors.Is(err, onesie.ErrValidation) {
			t.Errorf("error got %v, want ErrValidation", err)
		}
	})
}

func TestClientSystemOne(t *testing.T) {
	t.Parallel()

	routes := []struct {
		name     string
		provider onesie.Provider
		model    string
		wrap     func(answers string) string
	}{
		{name: "typesafe", provider: onesie.TypeSafe(), wrap: func(a string) string { return a }},
		{name: "openrouter", provider: onesie.OpenRouter(), wrap: func(a string) string { return a }},
		{name: "berget", provider: onesie.Berget(), wrap: func(a string) string { return a }},
		{name: "levanto", provider: onesie.Levanto(), wrap: func(a string) string { return a }},
		{
			name: "cloudflare's jev", provider: onesie.Cloudflare(), model: "typesafe/jev",
			wrap: func(a string) string { return `{"result":{"state":"Completed","result":` + a + `},"success":true}` },
		},
		{
			name: "cloudflare's clef", provider: onesie.Cloudflare(), model: "clef",
			wrap: func(a string) string { return `{"result":` + a + `,"success":true}` },
		},
	}

	broken := []struct {
		name   string
		answer string
	}{
		{name: "a noul answer that carries only its type", answer: `{"type":"noul"}`},
		{name: "a noul answer that sends value instead of noul", answer: `{"type":"noul","value":0.1}`},
		{name: "a noul answer whose noul is null", answer: `{"type":"noul","noul":null}`},
		{name: "a noul answer whose noul is a string", answer: `{"type":"noul","noul":"0.1"}`},
		{name: "a choice answer that carries only its type", answer: `{"type":"choice"}`},
		{name: "a choice answer with no probabilities", answer: `{"type":"choice","choice":"a"}`},
		{name: "a score answer that carries only its type", answer: `{"type":"score"}`},
		{name: "a score answer with no probabilities", answer: `{"type":"score","score":1}`},
		{name: "a box answer that carries only its type", answer: `{"type":"box"}`},
	}

	for _, route := range routes {
		for _, tc := range broken {
			t.Run("should refuse "+tc.name+" through "+route.name+" as it refuses a wrong type", func(t *testing.T) {
				t.Parallel()

				ask := onesie.Request{Model: route.model, State: "x", Questions: oneNoul()}
				respond := func(answer string) (string, error) {
					response := route.wrap(`{"model":"m","answers":{"q":` + answer +
						`},"usage":{"input_tokens":7,"output_tokens":3}}`)

					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						_, _ = io.WriteString(w, response)
					}))
					defer server.Close()

					client, _ := newTestClient(t, server.URL, onesie.WithProvider(route.provider))
					_, err := client.SystemOne(t.Context(), ask)

					return response, err
				}

				_, wrongTypeErr := respond(`{"type":"tarot"}`)
				response, err := respond(tc.answer)

				var want, got *onesie.ResponseError
				if !errors.As(wrongTypeErr, &want) {
					t.Fatalf("wrong type error = %v, want a ResponseError", wrongTypeErr)
				}

				if !errors.As(err, &got) || !errors.Is(err, onesie.ErrResponse) {
					t.Fatalf("error = %v, want a ResponseError matching ErrResponse", err)
				}

				const prefix = `onesie: 200 response could not be used: answer 'q' `
				if !strings.HasPrefix(want.Error(), prefix) || !strings.HasPrefix(got.Error(), prefix) {
					t.Errorf("errors = %q and %q, want both to start %q", want.Error(), got.Error(), prefix)
				}

				if got.Status != want.Status || string(got.Body) != response {
					t.Errorf("error = %+v, want it shaped like %+v holding %s", got, want, response)
				}

				for _, spent := range []*onesie.ResponseError{want, got} {
					if spent.Usage == nil || spent.Usage.InputTokens != 7 || spent.Usage.OutputTokens != 3 {
						t.Errorf("usage = %+v, want the 7 in and 3 out the response billed", spent.Usage)
					}
				}
			})
		}
	}

	t.Run("should send jev and clef through cloudflare's routes", func(t *testing.T) {
		t.Parallel()

		square, err := onesie.NewImage(squarePNG(t))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		const answers = `{"model":"x","answers":{"q":{"type":"noul","noul":0.9}},"usage":{"input_tokens":1,"output_tokens":0}}`

		tests := []struct {
			name     string
			model    string
			response string
			wantBody []string
			noBody   []string
		}{
			{
				name:     "should send jev its assets under input and read two levels",
				model:    "typesafe/jev",
				response: `{"result":{"state":"Completed","result":` + answers + `},"success":true}`,
				wantBody: []string{`{"model":"typesafe/jev","input":{"state":{"photo":"img"}`, `"assets":{"img":`},
				noBody:   []string{`"images"`, `"model":"typesafe/jev","input":{"state":{"photo":"img"},"model"`},
			},
			{
				name:     "should send clef numbered images and read one level",
				model:    "clef-flash",
				response: `{"result":` + answers + `,"success":true}`,
				wantBody: []string{
					`{"model":"@cf/cloudflare/clef-flash","input":{"state":{"photo":"image 1"}`,
					`"images":["data:image/png;base64,`,
				},
				noBody: []string{`"assets"`},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				var (
					mu   sync.Mutex
					path string
					sent string
				)

				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)

					mu.Lock()
					path, sent = r.URL.Path, string(body)
					mu.Unlock()

					_, _ = io.WriteString(w, tc.response)
				}))
				defer server.Close()

				client, _ := newTestClient(t, server.URL, onesie.WithProvider(onesie.Cloudflare()))

				result, err := client.SystemOne(t.Context(), onesie.Request{
					State:     map[string]any{"photo": "img"},
					Model:     tc.model,
					Questions: onesie.Questions{{ID: "q", Question: onesie.Noul{Instructions: "is there a red square in `photo`"}}},
					Assets:    onesie.Assets{"img": square},
				})
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if got, _ := result.Noul("q"); got == nil || got.Noul != 0.9 {
					t.Errorf("q = %+v, want 0.9", got)
				}

				mu.Lock()
				defer mu.Unlock()

				if path != "/run" {
					t.Errorf("path = %q, want /run", path)
				}

				for _, want := range tc.wantBody {
					if !strings.Contains(sent, want) {
						t.Errorf("body = %.300s, want it to contain %s", sent, want)
					}
				}

				for _, unwanted := range tc.noBody {
					if strings.Contains(sent, unwanted) {
						t.Errorf("body = %.300s, want it without %s", sent, unwanted)
					}
				}
			})
		}
	})

	for _, body := range []string{"<!DOCTYPE html><html></html>", "upstream said ok"} {
		t.Run("should name the base url when a 200 body is not JSON: "+body, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()

			client, _ := newTestClient(t, server.URL)

			_, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()})
			if !errors.Is(err, onesie.ErrResponse) {
				t.Fatalf("error got %v, want ErrResponse", err)
			}

			if !strings.Contains(err.Error(), "not JSON") || !strings.Contains(err.Error(), server.URL) {
				t.Errorf("error = %q, want it to say not JSON and name %s", err.Error(), server.URL)
			}
		})
	}

	t.Run("should report an empty 200 body", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL)

		_, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()})
		if !errors.Is(err, onesie.ErrResponse) || !strings.Contains(err.Error(), "empty response body") {
			t.Fatalf("error = %v, want ErrResponse naming an empty response body", err)
		}
	})

	t.Run("should fill the request id from x-generation-id under openrouter", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Generation-Id", "gen-dec-1")
			w.Header().Set("X-TypeSafe-Request-Id", "req_wrong")
			_, _ = io.WriteString(w, shortAnswer)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL, onesie.WithProvider(onesie.OpenRouter()))

		result, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.RequestID != "gen-dec-1" {
			t.Errorf("request id got %q, want gen-dec-1", result.RequestID)
		}
	})

	t.Run("should fill an api error request id from x-generation-id under openrouter", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Generation-Id", "gen-dec-2")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"bad","code":400}}`)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL, onesie.WithProvider(onesie.OpenRouter()))

		_, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()})

		var apiErr *onesie.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error got %v, want an APIError", err)
		}

		if apiErr.RequestID != "gen-dec-2" {
			t.Errorf("request id got %q, want gen-dec-2", apiErr.RequestID)
		}
	})

	t.Run("should fill the request id from x-request-id under berget", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Request-Id", "c9ee9293")
			w.Header().Set("X-TypeSafe-Request-Id", "req_wrong")
			_, _ = io.WriteString(w, shortAnswer)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL, onesie.WithProvider(onesie.Berget()))

		result, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.RequestID != "c9ee9293" {
			t.Errorf("request id got %q, want c9ee9293", result.RequestID)
		}
	})

	t.Run("should fill the request id from x-request-id under levanto", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Request-Id", "07114a10")
			w.Header().Set("X-TypeSafe-Request-Id", "req_wrong")
			_, _ = io.WriteString(w, shortAnswer)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL, onesie.WithProvider(onesie.Levanto()))

		result, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.RequestID != "07114a10" {
			t.Errorf("request id got %q, want 07114a10", result.RequestID)
		}
	})

	t.Run("should retry a berget 503 after the Retry-After it sends", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			if calls.Add(1) == 1 {
				w.Header().Set("Retry-After", "3")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, `{"error":{"message":"Temporarily unavailable",`+
					`"type":"server_error","code":null}}`)

				return
			}

			_, _ = io.WriteString(w, shortAnswer)
		}))
		defer server.Close()

		client, clock := newTestClient(t, server.URL, onesie.WithProvider(onesie.Berget()))

		if _, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if calls.Load() != 2 {
			t.Errorf("calls = %d, want 2", calls.Load())
		}

		if slept := clock.Slept(); len(slept) != 1 || slept[0] != 3*time.Second {
			t.Errorf("slept = %v, want one wait of 3s", slept)
		}
	})

	t.Run("should send the state questions and model then decode the answers", func(t *testing.T) {
		t.Parallel()

		var (
			mu      sync.Mutex
			body    map[string]json.RawMessage
			headers http.Header
		)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			headers = r.Header.Clone()

			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-TypeSafe-Request-Id", "req_abc")
			w.Header().Set("X-RateLimit-Remaining", "42")
			_, _ = io.WriteString(w, `{
				"model":"onesie-1.13.0",
				"answers":{"q":{"type":"noul","noul":0.95}},
				"usage":{"input_tokens":10,"output_tokens":2}
			}`)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL)

		result, err := client.SystemOne(t.Context(), onesie.Request{State: "charged twice", Questions: oneNoul()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()

		for _, key := range []string{"state", "model", "questions"} {
			if _, ok := body[key]; !ok {
				t.Errorf("request body is missing %q", key)
			}
		}

		if got := headers.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("authorization got %q", got)
		}

		for header, want := range map[string]string{
			"Content-Type": "application/json",
			"Accept":       "application/json",
		} {
			if got := headers.Get(header); got != want {
				t.Errorf("%s got %q, want %q", header, got, want)
			}
		}

		for _, header := range []string{"User-Agent", "X-TypeSafe-SDK", "X-TypeSafe-Runtime"} {
			if headers.Get(header) == "" {
				t.Errorf("%s was not set", header)
			}
		}

		if got := headers.Get("User-Agent"); got != "onesie-lib" {
			t.Errorf("User-Agent = %q, want onesie-lib", got)
		}

		if result.RequestID != "req_abc" {
			t.Errorf("request id got %q", result.RequestID)
		}

		if got := result.Header.Get("X-RateLimit-Remaining"); got != "42" {
			t.Errorf("response header got %q, want the rate limit value", got)
		}

		answer, err := result.Noul("q")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if answer.Noul != 0.95 {
			t.Errorf("noul got %v, want 0.95", answer.Noul)
		}
	})

	t.Run("should validate before sending anything", func(t *testing.T) {
		t.Parallel()

		var called atomic.Bool

		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			called.Store(true)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL)

		if _, err := client.SystemOne(t.Context(), onesie.Request{State: "x"}); err == nil {
			t.Fatalf("expected an error, got none")
		}

		if called.Load() {
			t.Errorf("the server was called despite invalid questions")
		}
	})

	t.Run("should not let a caller header clobber authorization", func(t *testing.T) {
		t.Parallel()

		var auth atomic.Value

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth.Store(r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, shortAnswer)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL)

		_, err := client.SystemOne(
			t.Context(),
			onesie.Request{State: "x", Questions: oneNoul()},
			onesie.WithRequestHeader("Authorization", "Bearer stolen"),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if auth.Load() != "Bearer sk-test" {
			t.Errorf("authorization got %v, want the client key", auth.Load())
		}
	})

	t.Run("should send a client level default header", func(t *testing.T) {
		t.Parallel()

		var seen atomic.Value

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen.Store(r.Header.Get("X-Trace"))
			_, _ = io.WriteString(w, shortAnswer)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL, onesie.WithHeader("X-Trace", "abc"))

		if _, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if seen.Load() != "abc" {
			t.Errorf("trace header got %v, want %q", seen.Load(), "abc")
		}
	})

	t.Run("should drop a caller supplied retry count header", func(t *testing.T) {
		t.Parallel()

		var seen atomic.Value

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen.Store(r.Header.Get("X-TypeSafe-Retry-Count"))
			_, _ = io.WriteString(w, shortAnswer)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL)

		_, err := client.SystemOne(
			t.Context(),
			onesie.Request{State: "x", Questions: oneNoul()},
			onesie.WithRequestHeader("X-TypeSafe-Retry-Count", "99"),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if seen.Load() != "" {
			t.Errorf("retry count got %v, want it dropped on the first attempt", seen.Load())
		}
	})

	t.Run("should reject a response missing an answer that was asked for", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"model":"m","answers":{},"usage":{}}`)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL)

		_, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()})

		if !errors.Is(err, onesie.ErrResponse) {
			t.Errorf("error got %v, want ErrResponse", err)
		}
	})

	t.Run("should carry the usage a response missing an answer billed", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"model":"m","answers":{},"usage":{"input_tokens":7,"output_tokens":3}}`)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL)

		_, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()})

		var unusable *onesie.ResponseError
		if !errors.As(err, &unusable) {
			t.Fatalf("error got %v, want a ResponseError", err)
		}

		if unusable.Usage == nil || unusable.Usage.InputTokens != 7 || unusable.Usage.OutputTokens != 3 {
			t.Errorf("usage = %+v, want 7 in and 3 out", unusable.Usage)
		}
	})

	t.Run("should refuse an answer that does not fit its question", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"model":"m","answers":{"q":{"type":"noul","noul":1.5}},"usage":{"input_tokens":7,"output_tokens":3}}`)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL)

		_, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()})

		var (
			unusable *onesie.ResponseError
			fault    *onesie.AnswerFault
		)

		const want = "onesie: 200 response could not be used: question 'q' answered 1.5, which lies outside [0,1]"
		if !errors.As(err, &unusable) || !errors.As(err, &fault) || fault.Question != "q" || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}

		if unusable.Usage == nil || unusable.Usage.InputTokens != 7 {
			t.Errorf("usage = %+v, want the 7 input tokens billed", unusable.Usage)
		}
	})

	usages := []struct {
		name     string
		response string
		want     *onesie.Usage
	}{
		{
			name:     "should carry the usage of a response whose answers are not an object",
			response: `{"model":"m","answers":[],"usage":{"input_tokens":3,"output_tokens":1}}`,
			want:     &onesie.Usage{InputTokens: 3, OutputTokens: 1},
		},
		{name: "should carry no usage when the response has none", response: `{"model":"m","answers":{}}`},
		{name: "should carry no usage when the usage does not decode", response: `{"answers":{},"usage":{"input_tokens":"7"}}`},
	}

	for _, tc := range usages {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tc.response)
			}))
			defer server.Close()

			client, _ := newTestClient(t, server.URL)

			_, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()})

			var unusable *onesie.ResponseError
			if !errors.As(err, &unusable) {
				t.Fatalf("error = %v, want a ResponseError", err)
			}

			if (unusable.Usage == nil) != (tc.want == nil) || (tc.want != nil && *unusable.Usage != *tc.want) {
				t.Errorf("usage = %+v, want %+v", unusable.Usage, tc.want)
			}
		})
	}

	t.Run("should refuse a response with no answers member", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"model":"m","usage":{}}`)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL)

		_, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()})

		const want = "onesie: 200 response could not be used: the response is not a System One result, since it has no answers"
		if err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}
	})

	t.Run("should reject an empty body on a 200", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL)

		_, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()})

		if !errors.Is(err, onesie.ErrResponse) {
			t.Errorf("error got %v, want ErrResponse", err)
		}
	})

	t.Run("should parse json sent without a json content type", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, `{"model":"onesie-1.13.0","answers":{"q":{"type":"noul","noul":0.5}},"usage":{}}`)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL)

		result, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.Model != "onesie-1.13.0" {
			t.Errorf("model got %q", result.Model)
		}
	})

	t.Run("should classify an oversized response body by its status", func(t *testing.T) {
		t.Parallel()

		oversized := "<html>" + strings.Repeat("a", 300) + "</html>"

		tests := []struct {
			name      string
			status    int
			wantCalls int
			wantErr   string
			sentinel  error
		}{
			{
				name: "should keep a 401 an authentication error", status: http.StatusUnauthorized, wantCalls: 1,
				wantErr: "onesie: 401 Unauthorized, with a response body over the 256 byte limit", sentinel: onesie.ErrAuthentication,
			},
			{
				name: "should retry a retryable status", status: http.StatusServiceUnavailable, wantCalls: 3,
				wantErr: "onesie: 503 Service Unavailable, with a response body over the 256 byte limit", sentinel: onesie.ErrServer,
			},
			{
				name: "should refuse a 2xx as a response error without retrying", status: http.StatusOK, wantCalls: 1,
				wantErr: "onesie: 200 response could not be used: the response body exceeded 256 bytes", sentinel: onesie.ErrResponse,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				client, _, transport := newScriptedClient(t, []scriptedReply{{status: tc.status, body: oversized}},
					onesie.WithMaxResponseBytes(256))

				_, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()})

				if err == nil || err.Error() != tc.wantErr || !errors.Is(err, tc.sentinel) || errors.Is(err, onesie.ErrConnection) {
					t.Errorf("error = %v, want %q matching %v", err, tc.wantErr, tc.sentinel)
				}

				if transport.calls() != tc.wantCalls {
					t.Errorf("call count got %d, want %d, %s", transport.calls(), tc.wantCalls, transport.attempts())
				}
			})
		}
	})

	for _, tc := range []struct {
		name    string
		err     error
		timeout bool
		want    string
	}{
		{name: "should read a deadline the transport reports as a timeout", err: context.DeadlineExceeded, timeout: true, want: "onesie: request to api.example:443 timed out"},
		{
			name: "should read a dial the network timed out as a timeout", timeout: true, want: "onesie: request to api.example:443 timed out",
			err: &net.OpError{Op: "dial", Net: "tcp", Err: netError{timeout: true}},
		},
		{
			name: "should leave a network error that is not a timeout a connection error",
			err:  &net.OpError{Op: "dial", Net: "tcp", Err: netError{}}, want: "onesie: could not reach api.example: network failed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, _, _ := newScriptedClient(t, []scriptedReply{{err: tc.err}})

			_, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()})

			var timedOut *onesie.TimeoutError
			if errors.As(err, &timedOut) != tc.timeout || !errors.Is(err, onesie.ErrConnection) || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q, a timeout: %t", err, tc.want, tc.timeout)
			}

			if tc.timeout && timedOut.Timeout != 0 {
				t.Errorf("Timeout = %s, want zero", timedOut.Timeout)
			}
		})
	}

	for _, tc := range []struct {
		name      string
		notBefore time.Time
		notAfter  time.Time
		detail    string
		want      string
	}{
		{
			name: "should name a certificate that expired by the client's clock", want: "onesie: could not reach api.example: certificate has expired",
			notBefore: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), notAfter: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			detail: "current time 2026-09-20T12:00:00Z is after 2026-01-01T00:00:00Z",
		},
		{
			name: "should name a certificate not yet valid by the client's clock", want: "onesie: could not reach api.example: certificate is not yet valid",
			notBefore: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), notAfter: time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC),
			detail: "current time 2026-09-20T12:00:00Z is before 2027-01-01T00:00:00Z",
		},
		{
			name: "should keep the general wording for a certificate valid by the client's clock", want: "onesie: could not reach api.example: certificate has expired or is not yet valid",
			notBefore: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), notAfter: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
			detail: "current time 2027-06-01T00:00:00Z is after 2027-01-01T00:00:00Z",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			refused := &tls.CertificateVerificationError{Err: x509.CertificateInvalidError{
				Cert: &x509.Certificate{NotBefore: tc.notBefore, NotAfter: tc.notAfter}, Reason: x509.Expired, Detail: tc.detail,
			}}
			client, _, _ := newScriptedClient(t, []scriptedReply{{err: refused}})

			if _, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()}); err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}

	t.Run("should retry a failed request when the policy allows it", func(t *testing.T) {
		t.Parallel()

		ask := onesie.Request{State: "x", Questions: oneNoul()}
		answer := scriptedReply{status: http.StatusOK, body: shortAnswer}
		dropped := scriptedReply{err: errors.New("connection reset by peer")}

		t.Run("should retry a 429 then succeed", func(t *testing.T) {
			t.Parallel()

			client, clock, transport := newScriptedClient(t, []scriptedReply{{status: http.StatusTooManyRequests}, answer})

			if _, err := client.SystemOne(t.Context(), ask); err != nil {
				t.Fatalf("unexpected error: %v, %s", err, transport.attempts())
			}

			if transport.calls() != 2 {
				t.Errorf("call count got %d, want 2, %s", transport.calls(), transport.attempts())
			}

			if slept := clock.Slept(); len(slept) != 1 || slept[0] != 500*time.Millisecond {
				t.Errorf("waits got %v, want one 500ms wait, %s", slept, transport.attempts())
			}

			if counts := transport.retryCounts(); len(counts) != 2 || counts[0] != "" || counts[1] != "1" {
				t.Errorf("retry count header got %v", counts)
			}
		})

		t.Run("should honour retry after on a 429", func(t *testing.T) {
			t.Parallel()

			client, clock, transport := newScriptedClient(t, []scriptedReply{
				{status: http.StatusTooManyRequests, header: http.Header{"Retry-After": {"2"}}}, answer,
			})

			if _, err := client.SystemOne(t.Context(), ask); err != nil {
				t.Fatalf("unexpected error: %v, %s", err, transport.attempts())
			}

			if transport.calls() != 2 {
				t.Errorf("call count got %d, want 2, %s", transport.calls(), transport.attempts())
			}

			if slept := clock.Slept(); len(slept) != 1 || slept[0] != 2*time.Second {
				t.Errorf("waits got %v, want one 2s wait, %s", slept, transport.attempts())
			}
		})

		t.Run("should floor a zero retry after rather than retrying immediately", func(t *testing.T) {
			t.Parallel()

			client, clock, transport := newScriptedClient(t, []scriptedReply{
				{status: http.StatusServiceUnavailable, header: http.Header{"Retry-After": {"0"}}}, answer,
			})

			if _, err := client.SystemOne(t.Context(), ask); err != nil {
				t.Fatalf("unexpected error: %v, %s", err, transport.attempts())
			}

			if transport.calls() != 2 {
				t.Errorf("call count got %d, want 2, %s", transport.calls(), transport.attempts())
			}

			if slept := clock.Slept(); len(slept) != 1 || slept[0] != 500*time.Millisecond {
				t.Errorf("waits got %v, want the 500ms floor, %s", slept, transport.attempts())
			}
		})

		t.Run("should not retry a 422", func(t *testing.T) {
			t.Parallel()

			client, _, transport := newScriptedClient(t, []scriptedReply{{
				status: http.StatusUnprocessableEntity,
				header: http.Header{"Content-Type": {"application/json"}},
				body:   `{"detail":[{"loc":["body","state"],"msg":"field required"}]}`,
			}})

			_, err := client.SystemOne(t.Context(), ask)

			if !errors.Is(err, onesie.ErrUnprocessableEntity) {
				t.Fatalf("error got %v, want ErrUnprocessableEntity, %s", err, transport.attempts())
			}

			if transport.calls() != 1 {
				t.Errorf("call count got %d, want 1, %s", transport.calls(), transport.attempts())
			}
		})

		t.Run("should give up after MaxRetries and return the last error", func(t *testing.T) {
			t.Parallel()

			client, clock, transport := newScriptedClient(t, []scriptedReply{{status: 529}})

			_, err := client.SystemOne(t.Context(), ask)

			if !errors.Is(err, onesie.ErrServer) {
				t.Fatalf("error got %v, want ErrServer, %s", err, transport.attempts())
			}

			if transport.calls() != 3 {
				t.Errorf("call count got %d, want 3, one attempt plus two retries, %s", transport.calls(), transport.attempts())
			}

			want := []time.Duration{500 * time.Millisecond, time.Second}
			slept := clock.Slept()

			if len(slept) != len(want) {
				t.Fatalf("waits got %v, want %v, %s", slept, want, transport.attempts())
			}

			for i, d := range want {
				if slept[i] != d {
					t.Errorf("wait %d got %v, want %v", i, slept[i], d)
				}
			}
		})

		t.Run("should retry a connection failure when the policy allows it", func(t *testing.T) {
			t.Parallel()

			client, clock, transport := newScriptedClient(t, []scriptedReply{dropped, answer})

			if _, err := client.SystemOne(t.Context(), ask); err != nil {
				t.Fatalf("unexpected error: %v, %s", err, transport.attempts())
			}

			if transport.calls() != 2 {
				t.Errorf("call count got %d, want 2, %s", transport.calls(), transport.attempts())
			}

			if len(clock.Slept()) != 1 {
				t.Errorf("waits got %v, want one, %s", clock.Slept(), transport.attempts())
			}
		})

		t.Run("should not retry a connection failure when RetryConnection is off", func(t *testing.T) {
			t.Parallel()

			policy := onesie.DefaultRetryPolicy()
			policy.RetryConnection = false

			client, _, transport := newScriptedClient(t, []scriptedReply{dropped}, onesie.WithRetry(policy))

			_, err := client.SystemOne(t.Context(), ask)

			if !errors.Is(err, onesie.ErrConnection) {
				t.Fatalf("error got %v, want ErrConnection, %s", err, transport.attempts())
			}

			if transport.calls() != 1 {
				t.Errorf("call count got %d, want 1, %s", transport.calls(), transport.attempts())
			}
		})

		t.Run("should respect a per call retry override", func(t *testing.T) {
			t.Parallel()

			client, _, transport := newScriptedClient(t, []scriptedReply{{status: http.StatusServiceUnavailable}})

			policy := client.RetryPolicy()
			policy.MaxRetries = 0

			if _, err := client.SystemOne(t.Context(), ask, onesie.WithRequestRetry(policy)); err == nil {
				t.Fatalf("expected an error, got none")
			}

			if transport.calls() != 1 {
				t.Errorf("call count got %d, want 1, %s", transport.calls(), transport.attempts())
			}
		})
	})

	t.Run("should stop a request that is cancelled or times out", func(t *testing.T) {
		t.Parallel()

		t.Run("should return the context error when the caller cancels", func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				client, _ := newTestClient(t, "https://api.example",
					onesie.WithHTTPClient(&http.Client{Transport: &stallingTransport{}}))

				ctx, cancel := context.WithCancel(t.Context())
				failed := make(chan error, 1)

				go func() {
					_, err := client.SystemOne(ctx, onesie.Request{State: "x", Questions: oneNoul()})
					failed <- err
				}()

				synctest.Wait()
				cancel()

				err := <-failed

				if !errors.Is(err, context.Canceled) {
					t.Errorf("error got %v, want context.Canceled", err)
				}

				var connection *onesie.ConnectionError
				if !errors.As(err, &connection) || err.Error() != "onesie: connection error: context canceled" {
					t.Errorf("error = %v, want a ConnectionError for the cancellation", err)
				}
			})
		})

		t.Run("should report a timeout as a connection error", func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				policy := onesie.DefaultRetryPolicy()
				policy.MaxRetries = 0

				client, _ := newTestClient(t, "https://api.example", onesie.WithRetry(policy),
					onesie.WithHTTPClient(&http.Client{Transport: &stallingTransport{}}))

				_, err := client.SystemOne(
					t.Context(),
					onesie.Request{State: "x", Questions: oneNoul()},
					onesie.WithRequestAttemptTimeout(30*time.Second),
				)

				if !errors.Is(err, onesie.ErrTimeout) {
					t.Fatalf("error got %v, want ErrTimeout", err)
				}

				if !errors.Is(err, onesie.ErrConnection) {
					t.Errorf("a timeout should also match ErrConnection")
				}
			})
		})

		t.Run("should give each attempt a fresh timeout and not one budget", func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				stalling := &stallingTransport{}
				client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: stalling}))

				started := time.Now()

				_, err := client.SystemOne(
					t.Context(),
					onesie.Request{State: "x", Questions: oneNoul()},
					onesie.WithRequestAttemptTimeout(30*time.Second),
				)

				if !errors.Is(err, onesie.ErrTimeout) {
					t.Fatalf("error got %v, want ErrTimeout", err)
				}

				if calls := stalling.calls.Load(); calls != 3 {
					t.Errorf("call count got %d, want 3", calls)
				}

				if elapsed := time.Since(started); elapsed != 90*time.Second {
					t.Errorf("elapsed = %v, want three whole 30s attempts", elapsed)
				}
			})
		})

		t.Run("should abort while waiting to retry", func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()

			ctx, cancel := context.WithCancel(t.Context())

			// The mock clock returns immediately, so cancel before the call to make the wait the
			// first thing that observes the cancellation.
			cancel()

			client, _ := newTestClient(t, server.URL)

			_, err := client.SystemOne(ctx, onesie.Request{State: "x", Questions: oneNoul()})

			if !errors.Is(err, context.Canceled) {
				t.Errorf("error got %v, want context.Canceled", err)
			}
		})

		t.Run("should honour a total timeout across retries", func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				stalling := &stallingTransport{unavailableFirst: true}
				client, _ := newTestClient(t, "https://api.example", onesie.WithTotalTimeout(5*time.Second),
					onesie.WithHTTPClient(&http.Client{Transport: stalling}))

				started := time.Now()

				_, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()})

				if elapsed := time.Since(started); elapsed != 5*time.Second {
					t.Errorf("elapsed = %v, want the 5s total", elapsed)
				}

				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("error got %v, want context.DeadlineExceeded", err)
				}

				var timeout *onesie.TimeoutError
				if !errors.As(err, &timeout) || timeout.Timeout != 5*time.Second || !errors.Is(err, onesie.ErrTimeout) {
					t.Errorf("error = %v, want a TimeoutError naming the 5s total", err)
				}

				if calls := stalling.calls.Load(); calls != 2 {
					t.Errorf("call count got %d, want a retry after the 503", calls)
				}
			})
		})
	})

	t.Run("should be safe for concurrent use", func(t *testing.T) {
		t.Parallel()

		t.Run("should serve many goroutines from one client", func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, shortAnswer)
			}))
			defer server.Close()

			client, _ := newTestClient(t, server.URL)

			var wg sync.WaitGroup

			for range 16 {
				wg.Add(1)

				go func() {
					defer wg.Done()

					if _, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()}); err != nil {
						t.Errorf("unexpected error: %v", err)
					}
				}()
			}

			wg.Wait()
		})
	})

	t.Run("should honour the Retry-After cap", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			retryAfter string
			cap        time.Duration
			wantCalls  int
			wantErr    bool
		}{
			{
				name:       "should stop immediately when the header exceeds the cap",
				retryAfter: "120",
				cap:        60 * time.Second,
				wantCalls:  1,
				wantErr:    true,
			},
			{
				name:       "should retry normally when the header is inside the cap",
				retryAfter: "1",
				cap:        60 * time.Second,
				wantCalls:  3,
				wantErr:    true,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				policy := onesie.DefaultRetryPolicy()
				policy.MaxRetryAfter = tc.cap

				client, _, transport := newScriptedClient(t, []scriptedReply{{
					status: http.StatusTooManyRequests,
					header: http.Header{"Retry-After": {tc.retryAfter}},
					body:   `{"error":{"message":"slow down"}}`,
				}}, onesie.WithRetry(policy))

				_, err := client.SystemOne(t.Context(), onesie.Request{
					State:     "hello",
					Questions: oneNoul(),
				})

				if tc.wantErr && err == nil {
					t.Fatal("expected an error, got none")
				}

				if got := transport.calls(); got != tc.wantCalls {
					t.Errorf("server calls = %d, want %d, %s", got, tc.wantCalls, transport.attempts())
				}

				if tc.wantCalls == 1 {
					var tooLong *onesie.RetryAfterError
					if !errors.As(err, &tooLong) {
						t.Fatalf("expected a *RetryAfterError, got %T: %v", err, err)
					}

					if tooLong.RetryAfter != 120*time.Second {
						t.Errorf("RetryAfter = %s, want 2m0s", tooLong.RetryAfter)
					}

					if !errors.Is(err, onesie.ErrRateLimit) {
						t.Error("expected errors.Is to reach ErrRateLimit through Unwrap")
					}
				}
			})
		}
	})

	t.Run("should send assets and the images header", func(t *testing.T) {
		t.Parallel()

		img, err := onesie.NewImage(squarePNG(t))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		transport := &recordingTransport{body: shortAnswer}
		client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))

		_, err = client.SystemOne(t.Context(), onesie.Request{
			State:     map[string]any{"photo": "image-a"},
			Questions: oneNoul(),
			Assets:    onesie.Assets{"image-a": img},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got := transport.last().Header.Get(onesie.HeaderImages); got != "true" {
			t.Errorf("%s = %q, want true", onesie.HeaderImages, got)
		}

		if !strings.Contains(transport.lastBody(), `"assets":{"image-a":{"type":"image","format":"png"`) {
			t.Errorf("body = %.200s, want the asset", transport.lastBody())
		}
	})

	t.Run("should send no images header without assets", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: shortAnswer}
		client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))

		if _, err := client.SystemOne(t.Context(), onesie.Request{State: "s", Questions: oneNoul()}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got := transport.last().Header.Get(onesie.HeaderImages); got != "" {
			t.Errorf("%s = %q, want it absent", onesie.HeaderImages, got)
		}
	})

	t.Run("should refuse a box without an image before any request", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: shortAnswer}
		client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))

		_, err := client.SystemOne(t.Context(), onesie.Request{
			State:     "s",
			Questions: onesie.Questions{{ID: "mug", Question: onesie.Box{Instructions: "the mug"}}},
		})
		if !errors.Is(err, onesie.ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}

		if len(transport.requests) != 0 {
			t.Errorf("requests = %d, want none", len(transport.requests))
		}
	})

	t.Run("should send inline parts and no assets or images header to openrouter", func(t *testing.T) {
		t.Parallel()

		img, err := onesie.NewImage(squarePNG(t))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		transport := &recordingTransport{body: shortAnswer}
		client, _ := newTestClient(t, "https://api.example",
			onesie.WithProvider(onesie.OpenRouter()), onesie.WithHTTPClient(&http.Client{Transport: transport}))

		_, err = client.SystemOne(t.Context(), onesie.Request{
			State:     map[string]any{"photo": "image-a"},
			Questions: oneNoul(),
			Assets:    onesie.Assets{"image-a": img},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		body := transport.lastBody()
		if !strings.Contains(body, `"state":{"photo":{"type":"image_url","image_url":{"url":"data:image/png;base64,`) {
			t.Errorf("body = %.200s, want the image as an inline part", body)
		}

		if strings.Contains(body, `"assets"`) {
			t.Errorf("body = %.200s, want no assets", body)
		}

		if got := transport.last().Header.Get(onesie.HeaderImages); got != "" {
			t.Errorf("%s = %q, want it absent", onesie.HeaderImages, got)
		}
	})

	for _, provider := range []onesie.Provider{onesie.Levanto(), onesie.Berget()} {
		t.Run("should refuse images and a box on "+provider.Name()+" before any request", func(t *testing.T) {
			t.Parallel()

			transport := &recordingTransport{body: shortAnswer}
			client, _ := newTestClient(t, "https://api.example",
				onesie.WithProvider(provider), onesie.WithHTTPClient(&http.Client{Transport: transport}))

			square, err := onesie.NewImage(squarePNG(t))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			assets := onesie.Assets{"image-a": square}

			for _, req := range []onesie.Request{
				{State: map[string]any{"photo": "image-a"}, Questions: oneNoul(), Assets: assets},
				{
					State:     map[string]any{"photo": "image-a"},
					Questions: onesie.Questions{{ID: "mug", Question: onesie.Box{Instructions: "the mug"}}},
					Assets:    assets,
				},
			} {
				_, err := client.SystemOne(t.Context(), req)
				if !errors.Is(err, onesie.ErrValidation) || !strings.Contains(err.Error(), "takes no images") {
					t.Errorf("error = %v, want the provider's refusal", err)
				}
			}

			if len(transport.requests) != 0 {
				t.Errorf("requests = %d, want none", len(transport.requests))
			}
		})
	}

	t.Run("should not write through the caller's options", func(t *testing.T) {
		t.Parallel()

		img, err := onesie.NewImage(squarePNG(t))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		transport := &recordingTransport{body: shortAnswer}
		client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))

		opts := make([]onesie.RequestOption, 1, 4)
		opts[0] = onesie.WithRequestHeader("X-Caller", "1")

		_, err = client.SystemOne(t.Context(), onesie.Request{
			State: map[string]any{"photo": "image-a"}, Questions: oneNoul(), Assets: onesie.Assets{"image-a": img},
		}, opts...)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if spare := opts[:cap(opts)][1]; spare != nil {
			t.Error("SystemOne wrote into the caller's spare capacity")
		}
	})

	for _, provider := range []onesie.Provider{onesie.TypeSafe(), onesie.OpenRouter(), onesie.Levanto(), onesie.Berget()} {
		t.Run("should send long context as the header and never the key on "+provider.Name(), func(t *testing.T) {
			t.Parallel()

			transport := &recordingTransport{body: shortAnswer}
			client, _ := newTestClient(t, "https://api.example",
				onesie.WithProvider(provider), onesie.WithHTTPClient(&http.Client{Transport: transport}))

			if _, err := client.SystemOne(t.Context(), onesie.Request{
				State: "s", Questions: oneNoul(), LongContext: true,
			}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := transport.last().Header.Get(onesie.HeaderLongContext); got != "true" {
				t.Errorf("%s = %q, want true", onesie.HeaderLongContext, got)
			}

			if body := transport.lastBody(); strings.Contains(body, "long_context") {
				t.Errorf("body = %s, want no long_context", body)
			}
		})
	}

	t.Run("should not write long context through the caller's options in SystemOne", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: shortAnswer}
		client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))

		opts := make([]onesie.RequestOption, 0, 4)

		if _, err := client.SystemOne(t.Context(), onesie.Request{State: "s", Questions: oneNoul(), LongContext: true}, opts...); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := client.SystemOne(t.Context(), onesie.Request{State: "s", Questions: oneNoul()}, opts...); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got := transport.last().Header.Get(onesie.HeaderLongContext); got != "" {
			t.Errorf("%s = %q on the second call, want it absent", onesie.HeaderLongContext, got)
		}
	})
}

func TestClientListModels(t *testing.T) {
	t.Parallel()

	t.Run("should show the URL without its query when the listing is not JSON", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "<!DOCTYPE html><html></html>")
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL, onesie.WithProvider(onesie.OpenRouter()))

		_, err := client.ListModels(t.Context())
		if !errors.Is(err, onesie.ErrResponse) || !strings.Contains(err.Error(), server.URL+"/v1/models") ||
			strings.Contains(err.Error(), "?") {
			t.Errorf("error = %v, want it to name %s/v1/models without a query", err, server.URL)
		}
	})

	t.Run("should name only the host when the connection fails", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.NotFoundHandler())
		server.Close()

		noRetry := onesie.DefaultRetryPolicy()
		noRetry.MaxRetries = 0

		client, _ := newTestClient(t, server.URL, onesie.WithProvider(onesie.OpenRouter()), onesie.WithRetry(noRetry))

		_, err := client.ListModels(t.Context())

		host := strings.TrimPrefix(server.URL, "http://")
		if !errors.Is(err, onesie.ErrConnection) || !strings.HasPrefix(err.Error(), "onesie: could not reach "+host+": ") ||
			strings.Contains(err.Error(), "/v1/models") {
			t.Errorf("error = %v, want it to name %s and the cause, without the path or query", err, host)
		}
	})

	t.Run("should log the URL without its query", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"data":[]}`)
		}))
		defer server.Close()

		var logged bytes.Buffer

		logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
		client, _ := newTestClient(t, server.URL, onesie.WithProvider(onesie.OpenRouter()), onesie.WithLogger(logger))

		_, _ = client.ListModels(t.Context())

		if !strings.Contains(logged.String(), "url="+server.URL+"/v1/models ") || strings.Contains(logged.String(), "output_modalities") {
			t.Errorf("log = %s, want the URL without its query", logged.String())
		}
	})

	t.Run("should name the base url when the listing is not JSON", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<!DOCTYPE html><html></html>")
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL)

		_, err := client.ListModels(t.Context())
		if !errors.Is(err, onesie.ErrResponse) {
			t.Fatalf("error got %v, want ErrResponse", err)
		}

		if !strings.Contains(err.Error(), "not JSON") || !strings.Contains(err.Error(), server.URL) {
			t.Errorf("error = %q, want it to say not JSON and name %s", err.Error(), server.URL)
		}
	})

	t.Run("should unwrap the models list", func(t *testing.T) {
		t.Parallel()

		var (
			mu          sync.Mutex
			method      string
			path        string
			contentType string
		)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			method, path = r.Method, r.URL.Path
			contentType = r.Header.Get("Content-Type")
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"models":[
				{"name":"jev-latest","description":"Flagship","release_date":"2026-08-01"}
			]}`)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL)

		models, err := client.ListModels(t.Context())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()

		if method != http.MethodGet || path != "/v1/models" {
			t.Errorf("request got %s %s", method, path)
		}

		if contentType != "" {
			t.Errorf("content type got %q, want it unset on a bodyless request", contentType)
		}

		if len(models) != 1 || models[0].Name != "jev-latest" {
			t.Errorf("models got %+v", models)
		}
	})

	t.Run("should refuse to follow a redirect, which would carry the key", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{"default", "injected"} {
			var followed atomic.Int32

			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				followed.Add(1)
				_, _ = io.WriteString(w, `{"models":[]}`)
			}))
			defer target.Close()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL+r.URL.Path, http.StatusFound)
			}))
			defer server.Close()

			var opts []onesie.Option
			if name == "injected" {
				opts = append(opts, onesie.WithHTTPClient(&http.Client{}))
			}

			client, _ := newTestClient(t, server.URL, opts...)

			_, err := client.ListModels(t.Context())

			var api *onesie.APIError
			if !errors.As(err, &api) || api.Status != http.StatusFound {
				t.Fatalf("%s client: error = %v, want an APIError with status 302", name, err)
			}

			want := "onesie: 302 redirect to " + target.URL + ", which onesie does not follow, " +
				"since the request carries the API key. Point the base URL at where the API is"
			if err.Error() != want {
				t.Errorf("%s client: error = %q, want %q", name, err.Error(), want)
			}

			if followed.Load() != 0 {
				t.Errorf("%s client: the redirect was followed", name)
			}
		}
	})

	t.Run("should reject an unexpected response shape", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"data":[]}`)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL)

		_, err := client.ListModels(t.Context())

		if !errors.Is(err, onesie.ErrResponse) {
			t.Errorf("error got %v, want ErrResponse", err)
		}
	})

	t.Run("should send no long context header on a models listing", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: `{"models":[]}`}
		client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))

		if _, err := client.ListModels(t.Context(), onesie.WithLongContext()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got := transport.last().Header.Get(onesie.HeaderLongContext); got != "" {
			t.Errorf("long context header = %q, want none on a GET that sends no question", got)
		}
	})
}

func TestClientVerifyKey(t *testing.T) {
	t.Parallel()

	const (
		typesafeListing = `{"models":[{"name":"jev-latest","description":"alias","release_date":"2026-08-01"}]}`
		levantoListing  = `{"models":[{"name":"levanto-sage-v1.4","description":"Levanto Sage decision model",` +
			`"release_date":"2026-10-04"},{"name":"sage-latest","description":"Alias of levanto-sage-v1.4.",` +
			`"release_date":"2026-10-04"}]}`
		cloudflareListing = `{"result":[{"name":"@cf/cloudflare/clef-flash","description":"Clef Flash",` +
			`"created_at":"2026-09-01 10:00:00"}],"success":true}`
		openRouterListing = `{"data":[{"id":"typesafe/jev-latest","description":"Jev","created":1785542400}]}`
		bergetListing     = `{"object":"list","data":[{"id":"Qwen/Qwen3.5-2B","model_type":"system-one",` +
			`"aliases":["systemone"],"release_date":"2026-09-23"}]}`
		bergetAnswer = `{"model":"Qwen/Qwen3.5-2B","answers":{"key":{"type":"noul","noul":0.9}},` +
			`"usage":{"input_tokens":22,"output_tokens":15}}`
		bergetBadKey = `{"error":{"code":"WALLET_NOT_SETUP","message":"No subscription found for this API key.",` +
			`"param":null,"type":"insufficient_quota"}}`
		openRouterKey    = `{"data":{"label":"sk-or-v1-abc...","usage":0,"limit":null}}`
		openRouterNoKey  = `{"error":{"message":"No auth credentials found","code":401}}`
		openRouterDenied = `{"error":{"message":"Forbidden","code":403}}`
		openRouterBroken = `{"error":{"message":"Internal Server Error","code":500}}`
	)

	tests := []struct {
		name       string
		provider   onesie.Provider
		basePath   string
		listing    string
		answer     string
		status     int
		keyBody    string
		keyStatus  int
		wantPaths  []string
		wantModels int
		wantErr    error
	}{
		{
			name:       "should only list the models on typesafe, whose list needs the key",
			provider:   onesie.TypeSafe(),
			listing:    typesafeListing,
			wantPaths:  []string{"GET /v1/models"},
			wantModels: 1,
		},
		{
			name:       "should only list the models on levanto, whose list needs the key",
			provider:   onesie.Levanto(),
			listing:    levantoListing,
			wantPaths:  []string{"GET /v1/models"},
			wantModels: 2,
		},
		{
			name:       "should only list the models on cloudflare, whose list needs the key",
			provider:   onesie.Cloudflare(),
			listing:    cloudflareListing,
			wantPaths:  []string{"GET /models/search"},
			wantModels: 2,
		},
		{
			name:       "should list the models and ask one question on berget, whose list needs no key",
			provider:   onesie.Berget(),
			listing:    bergetListing,
			answer:     bergetAnswer,
			wantPaths:  []string{"GET /v1/models/", "POST /v1/systemone"},
			wantModels: 1,
		},
		{
			name:      "should report a key berget refuses on the question",
			provider:  onesie.Berget(),
			listing:   bergetListing,
			answer:    bergetBadKey,
			status:    http.StatusPaymentRequired,
			wantPaths: []string{"GET /v1/models/", "POST /v1/systemone"},
			wantErr:   onesie.ErrPaymentRequired,
		},
		{
			name:       "should list the models and get the key endpoint on openrouter, whose list needs no key",
			provider:   onesie.OpenRouter(),
			listing:    openRouterListing,
			keyBody:    openRouterKey,
			wantPaths:  []string{"GET /v1/models", "GET /v1/key"},
			wantModels: 1,
		},
		{
			name:       "should get the openrouter key endpoint under a custom base URL",
			provider:   onesie.OpenRouter(),
			basePath:   "/proxy/api",
			listing:    openRouterListing,
			keyBody:    openRouterKey,
			wantPaths:  []string{"GET /proxy/api/v1/models", "GET /proxy/api/v1/key"},
			wantModels: 1,
		},
		{
			name:       "should get the openrouter key endpoint under a custom base URL with a trailing slash",
			provider:   onesie.OpenRouter(),
			basePath:   "/proxy/api/",
			listing:    openRouterListing,
			keyBody:    openRouterKey,
			wantPaths:  []string{"GET /proxy/api/v1/models", "GET /proxy/api/v1/key"},
			wantModels: 1,
		},
		{
			name:      "should refuse a key endpoint that answers 200 with a page that is not JSON",
			provider:  onesie.OpenRouter(),
			listing:   openRouterListing,
			keyBody:   `<!doctype html><html><body>Not the API</body></html>`,
			wantPaths: []string{"GET /v1/models", "GET /v1/key"},
			wantErr:   onesie.ErrResponse,
		},
		{
			name:      "should report a key the openrouter key endpoint refuses with 401 as a listing 401",
			provider:  onesie.OpenRouter(),
			listing:   openRouterListing,
			keyBody:   openRouterNoKey,
			keyStatus: http.StatusUnauthorized,
			wantPaths: []string{"GET /v1/models", "GET /v1/key"},
			wantErr:   onesie.ErrAuthentication,
		},
		{
			name:      "should report a key the openrouter key endpoint refuses with 403 as a listing 403",
			provider:  onesie.OpenRouter(),
			listing:   openRouterListing,
			keyBody:   openRouterDenied,
			keyStatus: http.StatusForbidden,
			wantPaths: []string{"GET /v1/models", "GET /v1/key"},
			wantErr:   onesie.ErrPermissionDenied,
		},
		{
			name:      "should retry and report a 500 from the openrouter key endpoint as a listing 500",
			provider:  onesie.OpenRouter(),
			listing:   openRouterListing,
			keyBody:   openRouterBroken,
			keyStatus: http.StatusInternalServerError,
			wantPaths: []string{"GET /v1/models", "GET /v1/key", "GET /v1/key", "GET /v1/key"},
			wantErr:   onesie.ErrServer,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var (
				mu         sync.Mutex
				paths      []string
				body       string
				keyBearers []string
			)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)

				mu.Lock()
				paths = append(paths, r.Method+" "+r.URL.Path)
				if r.Method == http.MethodPost {
					body = string(raw)
				}
				if strings.HasSuffix(r.URL.Path, "/key") {
					keyBearers = append(keyBearers, r.Header.Get("Authorization"))
				}
				mu.Unlock()

				w.Header().Set("Content-Type", "application/json")

				switch {
				case strings.HasSuffix(r.URL.Path, "/key"):
					writeStatusBody(w, tc.keyStatus, tc.keyBody)
				case r.Method == http.MethodGet:
					_, _ = io.WriteString(w, tc.listing)
				default:
					writeStatusBody(w, tc.status, tc.answer)
				}
			}))
			defer server.Close()

			client, _ := newTestClient(t, server.URL+tc.basePath, onesie.WithProvider(tc.provider))

			models, err := client.VerifyKey(t.Context())
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error got %v, want %v", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(models) != tc.wantModels {
				t.Errorf("models got %+v, want %d", models, tc.wantModels)
			}

			mu.Lock()
			defer mu.Unlock()

			if strings.Join(paths, ", ") != strings.Join(tc.wantPaths, ", ") {
				t.Errorf("requests got %v, want %v", paths, tc.wantPaths)
			}

			for _, bearer := range keyBearers {
				if bearer != "Bearer sk-test" {
					t.Errorf("key endpoint Authorization = %q, want the client's key", bearer)
				}
			}

			if body != "" && !strings.Contains(body, `"model":"`+onesie.Berget().DefaultModel()+`"`) {
				t.Errorf("question body = %s, want the default model", body)
			}

			if body != "" && !strings.Contains(body, `"state":"onesie key check"`) {
				t.Errorf("question body = %s, want the state onesie key check", body)
			}

			if tc.keyStatus >= http.StatusBadRequest {
				assertListingFailsAlike(t, tc.provider, tc.keyStatus, tc.keyBody, err, len(keyBearers))
			}
		})
	}
}

func writeStatusBody(w http.ResponseWriter, status int, body string) {
	if status != 0 {
		w.WriteHeader(status)
	}

	_, _ = io.WriteString(w, body)
}

func assertListingFailsAlike(
	t *testing.T,
	provider onesie.Provider,
	status int,
	body string,
	keyErr error,
	keyRequests int,
) {
	t.Helper()

	var listings atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		listings.Add(1)
		w.Header().Set("Content-Type", "application/json")
		writeStatusBody(w, status, body)
	}))
	defer server.Close()

	client, _ := newTestClient(t, server.URL, onesie.WithProvider(provider))

	_, listErr := client.ListModels(t.Context())

	var fromKey, fromList *onesie.APIError
	if !errors.As(keyErr, &fromKey) || !errors.As(listErr, &fromList) {
		t.Fatalf("errors got %T and %T, want an *APIError from both", keyErr, listErr)
	}

	if fromKey.Status != fromList.Status || fromKey.Error() != fromList.Error() {
		t.Errorf("key endpoint error = %d %q, listing error = %d %q, want the same",
			fromKey.Status, fromKey.Error(), fromList.Status, fromList.Error())
	}

	if int(listings.Load()) != keyRequests {
		t.Errorf("listing requests = %d, key endpoint requests = %d, want the same", listings.Load(), keyRequests)
	}
}

func TestWithAttemptObserver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		statuses    []int
		wantReports []int
	}{
		{
			name:        "should report one attempt for a clean request",
			statuses:    []int{http.StatusOK},
			wantReports: []int{200},
		},
		{
			name:        "should report every attempt including the retries",
			statuses:    []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusOK},
			wantReports: []int{429, 500, 200},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var index atomic.Int64

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				status := tc.statuses[int(index.Add(1))-1]
				w.WriteHeader(status)

				body := `{"error":{"message":"nope"}}`
				if status == http.StatusOK {
					body = `{"model":"onesie-1.0.0","answers":{"q":{"type":"noul","noul":0.5}},` +
						`"usage":{"input_tokens":1,"output_tokens":1}}`
				}

				if _, err := w.Write([]byte(body)); err != nil {
					t.Errorf("writing stub response: %v", err)
				}
			}))
			defer srv.Close()

			var (
				mu      sync.Mutex
				reports []int
			)

			client, _ := newTestClient(t, srv.URL, onesie.WithAttemptObserver(func(a onesie.Attempt) {
				mu.Lock()
				defer mu.Unlock()

				reports = append(reports, a.Status)
			}))

			if _, err := client.SystemOne(t.Context(), onesie.Request{State: "x", Questions: oneNoul()}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			mu.Lock()
			defer mu.Unlock()

			if !slices.Equal(reports, tc.wantReports) {
				t.Errorf("observed statuses = %v, want %v", reports, tc.wantReports)
			}
		})
	}

	for _, tc := range []struct {
		name    string
		opts    []onesie.Option
		cancels bool
	}{
		{name: "should observe a cancelled call as the ConnectionError it returns", cancels: true},
		{name: "should observe a call past its total timeout as the TimeoutError it returns", opts: []onesie.Option{onesie.WithTotalTimeout(5 * time.Second)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				var (
					mu       sync.Mutex
					observed []error
				)

				opts := append([]onesie.Option{
					onesie.WithHTTPClient(&http.Client{Transport: &stallingTransport{}}),
					onesie.WithAttemptObserver(func(a onesie.Attempt) {
						mu.Lock()
						defer mu.Unlock()

						observed = append(observed, a.Err)
					}),
				}, tc.opts...)
				client, _ := newTestClient(t, "https://api.example", opts...)

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				failed := make(chan error, 1)

				go func() {
					_, err := client.SystemOne(ctx, onesie.Request{State: "x", Questions: oneNoul()})
					failed <- err
				}()

				if tc.cancels {
					synctest.Wait()
					cancel()
				}

				err := <-failed

				mu.Lock()
				defer mu.Unlock()

				if len(observed) != 1 || observed[0] == nil || err == nil {
					t.Fatalf("observed = %v, error = %v, want one observed failure", observed, err)
				}

				if fmt.Sprintf("%T %v", observed[0], observed[0]) != fmt.Sprintf("%T %v", err, err) {
					t.Errorf("observed %T %v, want the returned %T %v", observed[0], observed[0], err, err)
				}
			})
		})
	}

	t.Run("should observe an attempt whose body could not be read", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			body string
			want int
		}{
			{
				name: "should report an attempt whose body was too large to read",
				body: strings.Repeat("x", 64),
				want: 1,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					if _, err := io.WriteString(w, tc.body); err != nil {
						t.Errorf("writing stub response: %v", err)
					}
				}))
				defer srv.Close()

				var (
					mu       sync.Mutex
					observed int
				)

				client, err := onesie.New(
					onesie.WithAPIKey("test"),
					onesie.WithBaseURL(srv.URL),
					onesie.WithMaxResponseBytes(8),
					onesie.WithRetry(noRetries()),
					onesie.WithAttemptObserver(func(onesie.Attempt) {
						mu.Lock()
						defer mu.Unlock()

						observed++
					}),
				)
				if err != nil {
					t.Fatalf("New: %v", err)
				}

				if _, reqErr := client.SystemOne(
					t.Context(), onesie.Request{State: "x", Questions: oneNoul()}); reqErr == nil {
					t.Fatal("SystemOne succeeded, want an oversized body error")
				}

				mu.Lock()
				defer mu.Unlock()

				// A round trip the server answered is an attempt, whatever onesie could do with the body.
				// An observer that never saw it would under count, and a --stats run would carry a
				// terminal failure with no attempt behind it.
				if observed != tc.want {
					t.Errorf("observed %d attempts, want %d", observed, tc.want)
				}
			})
		}
	})

	t.Run("should report the timeout each attempt ran with", func(t *testing.T) {
		t.Parallel()

		square, err := onesie.NewImage(squarePNG(t))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		plain := onesie.Request{State: "s", Questions: oneNoul()}
		withImage := onesie.Request{State: "image-a", Questions: oneNoul(), Assets: onesie.Assets{"image-a": square}}

		rawWithImage, err := onesie.MarshalBody(withImage)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tests := []struct {
			name    string
			opts    []onesie.Option
			reqOpts []onesie.RequestOption
			req     *onesie.Request
			raw     json.RawMessage
			want    time.Duration
		}{
			{name: "should use the default for a request without images", req: &plain, want: onesie.DefaultAttemptTimeout},
			{name: "should use the asset timeout for a request with images", req: &withImage, want: onesie.AssetAttemptTimeout},
			{name: "should use the asset timeout for a raw body with images", raw: rawWithImage, want: onesie.AssetAttemptTimeout},
			{
				name: "should use the default for a raw body with empty assets",
				raw:  json.RawMessage(`{"state":"s","questions":{},"assets":{}}`), want: onesie.DefaultAttemptTimeout,
			},
			{
				name: "should let WithAttemptTimeout set both", opts: []onesie.Option{onesie.WithAttemptTimeout(5 * time.Second)},
				req: &withImage, want: 5 * time.Second,
			},
			{
				name: "should let WithRequestAttemptTimeout set both", reqOpts: []onesie.RequestOption{onesie.WithRequestAttemptTimeout(3 * time.Second)},
				req: &withImage, want: 3 * time.Second,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				var seen atomic.Int64

				transport := &recordingTransport{body: shortAnswer}
				opts := append([]onesie.Option{
					onesie.WithHTTPClient(&http.Client{Transport: transport}),
					onesie.WithAttemptObserver(func(a onesie.Attempt) { seen.Store(int64(a.Timeout)) }),
				}, tc.opts...)
				client, _ := newTestClient(t, "https://api.example", opts...)

				var err error
				if tc.req != nil {
					_, err = client.SystemOne(t.Context(), *tc.req, tc.reqOpts...)
				} else {
					_, err = client.SystemOneRaw(t.Context(), tc.raw, tc.reqOpts...)
				}

				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if got := time.Duration(seen.Load()); got != tc.want {
					t.Errorf("Attempt.Timeout = %v, want %v", got, tc.want)
				}
			})
		}
	})
}

func TestClientAttemptTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []onesie.Option
		want time.Duration
	}{
		{name: "should report the default when no option set one", want: onesie.DefaultAttemptTimeout},
		{name: "should report the option's timeout", opts: []onesie.Option{onesie.WithAttemptTimeout(time.Second)}, want: time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, _ := newTestClient(t, "https://api.example", tc.opts...)

			if got := client.AttemptTimeout(); got != tc.want {
				t.Errorf("AttemptTimeout() = %v, want %v", got, tc.want)
			}
		})
	}
}

func noRetries() onesie.RetryPolicy {
	policy := onesie.DefaultRetryPolicy()
	policy.MaxRetries = 0

	return policy
}

func TestClientSystemOneRaw(t *testing.T) {
	t.Parallel()

	t.Run("should send a raw clef body with empty assets without them", func(t *testing.T) {
		t.Parallel()

		var sent string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			sent = string(body)
			_, _ = io.WriteString(w, `{"result":{"model":"clef","answers":{},"usage":{}},"success":true}`)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL, onesie.WithProvider(onesie.Cloudflare()))

		got, err := client.SystemOneRaw(t.Context(), json.RawMessage(`{"state":"s","model":"clef","questions":{},"assets":{}}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if string(got) != `{"model":"clef","answers":{},"usage":{}}` {
			t.Errorf("body = %s, want the unwrapped result", got)
		}

		if sent != `{"model":"@cf/cloudflare/clef","input":{"state":"s","questions":{}}}` {
			t.Errorf("sent = %s", sent)
		}
	})

	t.Run("should send a raw clef body in clef's own shape as written", func(t *testing.T) {
		t.Parallel()

		var sent string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			sent = string(body)
			_, _ = io.WriteString(w, `{"result":{"model":"clef","answers":{},"usage":{}},"success":true}`)
		}))
		defer server.Close()

		client, _ := newTestClient(t, server.URL, onesie.WithProvider(onesie.Cloudflare()))

		if _, err := client.SystemOneRaw(t.Context(),
			json.RawMessage(`{"state":"s","model":"clef","questions":{},"images":["data:image/png;base64,AQ=="]}`)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := `{"model":"@cf/cloudflare/clef","input":{"state":"s","questions":{},"images":["data:image/png;base64,AQ=="]}}`
		if sent != want {
			t.Errorf("sent = %s, want %s", sent, want)
		}
	})

	for _, tc := range []struct {
		name  string
		model string
		want  string
	}{
		{
			name:  "should send a raw jev body's assets as written",
			model: "typesafe/jev",
			want: `{"model":"typesafe/jev","input":{"state":{"p":"r"},"questions":{"q":{"type":"noul","instructions":"q"}},` +
				`"assets":{"r":{"type":"image","format":"png","data_base64":"AQ=="}}}}`,
		},
		{
			name:  "should send a raw clef body's assets as numbered images",
			model: "clef",
			want: `{"model":"@cf/cloudflare/clef","input":{"state":{"p":"image 1"},"questions":{"q":{"type":"noul","instructions":"q"}},` +
				`"images":["data:image/png;base64,AQ=="]}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var sent string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				sent = string(body)
				_, _ = io.WriteString(w, `{"result":{"model":"m","answers":{},"usage":{}},"success":true}`)
			}))
			defer server.Close()

			client, _ := newTestClient(t, server.URL, onesie.WithProvider(onesie.Cloudflare()))

			body := `{"state":{"p":"r"},"model":"` + tc.model + `","questions":{"q":{"type":"noul","instructions":"q"}},` +
				`"assets":{"r":{"type":"image","format":"png","data_base64":"AQ=="}}}`
			if _, err := client.SystemOneRaw(t.Context(), json.RawMessage(body)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if sent != tc.want {
				t.Errorf("sent = %s\nwant %s", sent, tc.want)
			}
		})
	}

	compact := json.RawMessage(`{"state":"hi","model":"jev-latest","questions":{}}`)

	tests := []struct {
		name     string
		status   int
		response string
		send     json.RawMessage
		wantSent string
		wantErr  bool
	}{
		{
			name:     "should return the response body unchanged",
			status:   http.StatusOK,
			response: `{"model":"onesie-1.0.0","answers":{"q":{"type":"noul","noul":0.25}}}`,
		},
		{
			name:     "should return an api error for a non 2xx",
			status:   http.StatusUnprocessableEntity,
			response: `{"error":{"message":"state too large"}}`,
			wantErr:  true,
		},
		{
			name:     "should forward a body without HTML escaping it",
			status:   http.StatusOK,
			response: `{"model":"onesie-1.0.0","answers":{}}`,
			send:     json.RawMessage(`{"state":"a < b & c > d"}`),
			wantSent: `{"model":"jev-latest","state":"a < b & c > d"}`,
		},
		{
			name:     "should forward the line separators json.Marshal would escape",
			status:   http.StatusOK,
			response: `{"model":"onesie-1.0.0","answers":{}}`,
			send:     json.RawMessage("{\"state\":\"a\u2028b\u2029c\"}"),
			wantSent: "{\"model\":\"jev-latest\",\"state\":\"a\u2028b\u2029c\"}",
		},
		{
			name:     "should compact a pretty printed body without reordering it",
			status:   http.StatusOK,
			response: `{"model":"onesie-1.0.0","answers":{}}`,
			send:     json.RawMessage("{\n  \"state\": \"hi\",\n  \"model\": \"jev-latest\"\n}"),
			wantSent: `{"state":"hi","model":"jev-latest"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var (
				mu   sync.Mutex
				sent []byte
			)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("reading request body: %v", err)
				}

				mu.Lock()
				sent = body
				mu.Unlock()

				w.WriteHeader(tc.status)
				if _, err := w.Write([]byte(tc.response)); err != nil {
					t.Errorf("writing stub response: %v", err)
				}
			}))
			defer srv.Close()

			client, _ := newTestClient(t, srv.URL)

			body := tc.send
			if body == nil {
				body = compact
			}

			got, err := client.SystemOneRaw(t.Context(), body)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error, got none")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			wantSent := tc.wantSent
			if wantSent == "" {
				wantSent = string(body)
			}

			mu.Lock()
			gotSent := string(sent)
			mu.Unlock()

			if gotSent != wantSent {
				t.Errorf("sent body = %s, want %s", gotSent, wantSent)
			}

			if string(got) != tc.response {
				t.Errorf("returned body = %s, want %s", got, tc.response)
			}
		})
	}

	for _, provider := range onesie.Providers() {
		for _, body := range []string{"", "<!DOCTYPE html><html></html>"} {
			t.Run("should return a 200 body it cannot read unchanged on "+provider.Name()+": "+body, func(t *testing.T) {
				t.Parallel()

				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = io.WriteString(w, body)
				}))
				defer server.Close()

				client, _ := newTestClient(t, server.URL, onesie.WithProvider(provider))

				got, err := client.SystemOneRaw(t.Context(), json.RawMessage(`{"state":"s","model":"m","questions":{}}`))
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if string(got) != body {
					t.Errorf("body = %q, want %q", got, body)
				}
			})
		}
	}

	t.Run("should refuse a JSON body the cloudflare route cannot unwrap", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: `{"success":true}`}
		client, _ := newTestClient(t, "https://api.example",
			onesie.WithProvider(onesie.Cloudflare()), onesie.WithHTTPClient(&http.Client{Transport: transport}))

		_, err := client.SystemOneRaw(t.Context(), json.RawMessage(`{"state":"s","questions":{}}`))

		var respErr *onesie.ResponseError
		if !errors.As(err, &respErr) || respErr.Status != http.StatusOK || !strings.Contains(err.Error(), "expected a result object") {
			t.Fatalf("error = %v, want a 200 ResponseError saying it expected a result object", err)
		}
	})

	withAssets := json.RawMessage(`{"state":{"photo":"r"},"model":"m","questions":{"q":{"type":"noul",` +
		`"instructions":"q"}},"assets":{"r":{"type":"image","format":"png","data_base64":"AQ=="}}}`)

	t.Run("should send a raw body's assets and the images header to typesafe", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: shortAnswer}
		client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))

		if _, err := client.SystemOneRaw(t.Context(), withAssets); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got := transport.lastBody(); got != string(withAssets) {
			t.Errorf("body = %s, want it as sent", got)
		}

		if got := transport.last().Header.Get(onesie.HeaderImages); got != "true" {
			t.Errorf("%s = %q, want true", onesie.HeaderImages, got)
		}
	})

	t.Run("should rewrite a raw body into inline parts for openrouter", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: shortAnswer}
		client, _ := newTestClient(t, "https://api.example",
			onesie.WithProvider(onesie.OpenRouter()), onesie.WithHTTPClient(&http.Client{Transport: transport}))

		if _, err := client.SystemOneRaw(t.Context(), withAssets); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := `{"state":{"photo":{"type":"image_url","image_url":{"url":"data:image/png;base64,AQ=="}}},` +
			`"model":"m","questions":{"q":{"type":"noul","instructions":"q"}}}`
		if got := transport.lastBody(); got != want {
			t.Errorf("body\ngot  %s\nwant %s", got, want)
		}
	})

	t.Run("should refuse a raw body with images on levanto before any request", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: shortAnswer}
		client, _ := newTestClient(t, "https://api.example",
			onesie.WithProvider(onesie.Levanto()), onesie.WithHTTPClient(&http.Client{Transport: transport}))

		_, err := client.SystemOneRaw(t.Context(), withAssets)
		if !errors.Is(err, onesie.ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}

		if len(transport.requests) != 0 {
			t.Errorf("requests = %d, want none", len(transport.requests))
		}
	})

	for _, provider := range []onesie.Provider{onesie.TypeSafe(), onesie.OpenRouter(), onesie.Levanto(), onesie.Berget()} {
		t.Run("should send a raw text body as it is on "+provider.Name(), func(t *testing.T) {
			t.Parallel()

			transport := &recordingTransport{body: shortAnswer}
			client, _ := newTestClient(t, "https://api.example",
				onesie.WithProvider(provider), onesie.WithHTTPClient(&http.Client{Transport: transport}))

			body := json.RawMessage(`{"state":"a < b","model":"m","questions":{},"assets":{}}`)
			if _, err := client.SystemOneRaw(t.Context(), body); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := transport.lastBody(); got != string(body) {
				t.Errorf("body = %s, want it as sent", got)
			}
		})
	}

	for _, provider := range []onesie.Provider{onesie.TypeSafe(), onesie.OpenRouter()} {
		t.Run("should send a raw body's long_context as the header on "+provider.Name(), func(t *testing.T) {
			t.Parallel()

			transport := &recordingTransport{body: shortAnswer}
			client, _ := newTestClient(t, "https://api.example",
				onesie.WithProvider(provider), onesie.WithHTTPClient(&http.Client{Transport: transport}))

			body := json.RawMessage(`{"state":"s","model":"m","long_context":true,"questions":{}}`)
			if _, err := client.SystemOneRaw(t.Context(), body); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := transport.lastBody(); got != `{"state":"s","model":"m","questions":{}}` {
				t.Errorf("body = %s, want it without long_context", got)
			}

			if got := transport.last().Header.Get(onesie.HeaderLongContext); got != "true" {
				t.Errorf("%s = %q, want true", onesie.HeaderLongContext, got)
			}
		})
	}

	for _, tc := range []struct {
		name, body, want string
	}{
		{name: "should send the default model in a raw body that names none", body: `{"state":"s"}`, want: `{"model":"custom","state":"s"}`},
		{name: "should send the default model in place of a blank one", body: `{"state":"s","model":" "}`, want: `{"state":"s","model":"custom"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			transport := &recordingTransport{body: shortAnswer}
			client, _ := newTestClient(t, "https://api.example",
				onesie.WithDefaultModel("custom"), onesie.WithHTTPClient(&http.Client{Transport: transport}))

			if _, err := client.SystemOneRaw(t.Context(), json.RawMessage(tc.body)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			prepared, err := onesie.TypeSafe().PrepareRaw(json.RawMessage(tc.body), client.DefaultModel())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := transport.lastBody(); got != tc.want || got != string(prepared.Bytes()) {
				t.Errorf("body = %s, want %s, as PrepareRaw prints it", got, tc.want)
			}
		})
	}

	t.Run("should send a prepared body as it sends the body itself", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: shortAnswer}
		client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))
		body := json.RawMessage(`{"model":"","state":"s","model":"m","long_context":true}`)

		if _, err := client.SystemOneRaw(t.Context(), body); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		direct := transport.lastBody()

		prepared, err := onesie.TypeSafe().PrepareRaw(body, client.DefaultModel())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := client.SystemOneRaw(t.Context(), prepared.Bytes()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if transport.lastBody() != direct || direct != `{"state":"s","model":"m"}` {
			t.Errorf("sent %s then %s, want both {\"state\":\"s\",\"model\":\"m\"}", direct, transport.lastBody())
		}
	})

	t.Run("should not write long context through the caller's options", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: shortAnswer}
		client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))

		opts := make([]onesie.RequestOption, 0, 4)

		if _, err := client.SystemOneRaw(t.Context(), json.RawMessage(`{"state":"s","long_context":true}`), opts...); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := client.SystemOneRaw(t.Context(), json.RawMessage(`{"state":"s"}`), opts...); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got := transport.last().Header.Get(onesie.HeaderLongContext); got != "" {
			t.Errorf("%s = %q on the second call, want it absent", onesie.HeaderLongContext, got)
		}
	})

	t.Run("should refuse a bad long_context before the provider's image refusal", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: shortAnswer}
		client, _ := newTestClient(t, "https://api.example",
			onesie.WithProvider(onesie.Levanto()), onesie.WithHTTPClient(&http.Client{Transport: transport}))

		_, err := client.SystemOneRaw(t.Context(), json.RawMessage(
			`{"state":"r","questions":{},"long_context":"yes","assets":{"r":{"format":"png","data_base64":"AQ=="}}}`))
		if err == nil || !strings.Contains(err.Error(), "long_context must be true or false") {
			t.Fatalf("error = %v, want the long_context refusal", err)
		}

		if len(transport.requests) != 0 {
			t.Errorf("requests = %d, want none", len(transport.requests))
		}
	})

	t.Run("should refuse an unreferenced asset alike through SystemOne and SystemOneRaw on every provider", func(t *testing.T) {
		t.Parallel()

		img := onesie.Image{Format: "png", Data: []byte{1}}
		req := onesie.Request{State: "no image named here", Model: "m", Questions: oneNoul(), Assets: onesie.Assets{"image-x": img}}

		body, err := onesie.MarshalBody(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var messages []string

		for _, provider := range []onesie.Provider{onesie.TypeSafe(), onesie.OpenRouter()} {
			transport := &recordingTransport{body: shortAnswer}
			client, _ := newTestClient(t, "https://api.example",
				onesie.WithProvider(provider), onesie.WithHTTPClient(&http.Client{Transport: transport}))

			_, rawErr := client.SystemOneRaw(t.Context(), body)
			_, typedErr := client.SystemOne(t.Context(), req)

			for _, err := range []error{rawErr, typedErr} {
				if !errors.Is(err, onesie.ErrValidation) {
					t.Fatalf("%s: error = %v, want ErrValidation", provider.Name(), err)
				}

				messages = append(messages, err.Error())
			}

			if len(transport.requests) != 0 {
				t.Errorf("%s: requests = %d, want none", provider.Name(), len(transport.requests))
			}
		}

		for _, message := range messages[1:] {
			if message != messages[0] {
				t.Errorf("messages differ: %q and %q", messages[0], message)
			}
		}
	})

	for _, bad := range []struct {
		name, body, want string
	}{
		{name: "a body that is not JSON", body: `{"state":1,`, want: "onesie: the request body is not JSON: "},
		{name: "a body that is not UTF-8", body: "{\"long_context\":\"yes\",\"state\":\"\xff\"}", want: "onesie: the request body is not valid UTF-8"},
		{
			name: "a question of an unknown type", body: `{"state":"s","questions":{"q":{"type":"tarot"}}}`,
			want: "onesie: question 'q' has the type 'tarot', which is not noul, choice, score or box",
		},
	} {
		for _, provider := range onesie.Providers() {
			t.Run("should refuse "+bad.name+" on "+provider.Name()+" before any request", func(t *testing.T) {
				t.Parallel()

				transport := &recordingTransport{body: shortAnswer}
				client, _ := newTestClient(t, "https://api.example",
					onesie.WithProvider(provider), onesie.WithHTTPClient(&http.Client{Transport: transport}))

				_, err := client.SystemOneRaw(t.Context(), json.RawMessage(bad.body))

				var validation *onesie.ValidationError
				if !errors.As(err, &validation) || !errors.Is(err, onesie.ErrValidation) || !strings.HasPrefix(err.Error(), bad.want) {
					t.Fatalf("error = %v, want a ValidationError starting %q", err, bad.want)
				}

				if len(transport.requests) != 0 {
					t.Errorf("requests = %d, want none", len(transport.requests))
				}
			})
		}
	}

	for _, bad := range []struct {
		name, body, want string
	}{
		{name: "a bad format", body: `{"state":"r","questions":{},"assets":{"r":{"format":"gif","data_base64":"AQ=="}}}`, want: "has format 'gif'"},
		{name: "data that is not a string", body: `{"state":"r","questions":{},"assets":{"r":{"format":"png","data_base64":1}}}`, want: "needs data_base64 as a string"},
		{name: "assets that are not an object", body: `{"state":"r","questions":{},"assets":[1]}`, want: "assets must be an object of images"},
	} {
		for _, provider := range []onesie.Provider{onesie.TypeSafe(), onesie.OpenRouter()} {
			t.Run("should refuse "+bad.name+" in a raw body on "+provider.Name()+" before any request", func(t *testing.T) {
				t.Parallel()

				transport := &recordingTransport{body: shortAnswer}
				client, _ := newTestClient(t, "https://api.example",
					onesie.WithProvider(provider), onesie.WithHTTPClient(&http.Client{Transport: transport}))

				_, err := client.SystemOneRaw(t.Context(), json.RawMessage(bad.body))
				if !errors.Is(err, onesie.ErrValidation) || !strings.Contains(err.Error(), bad.want) {
					t.Fatalf("error = %v, want %q", err, bad.want)
				}

				if len(transport.requests) != 0 {
					t.Errorf("requests = %d, want none", len(transport.requests))
				}
			})
		}
	}
}

func TestClientSendPrepared(t *testing.T) {
	t.Parallel()

	const asked = `{"state":"s","questions":{"q":{"type":"noul","instructions":"Urgent?"}}}`

	prepare := func(t *testing.T, provider onesie.Provider, client *onesie.Client, body string, opts ...onesie.RequestOption) *onesie.PreparedBody {
		t.Helper()

		prepared, err := provider.PrepareRaw(json.RawMessage(body), client.DefaultModel(), opts...)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		return prepared
	}

	t.Run("should send the prepared body and return the checked result and the response body", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: shortAnswer}
		client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))

		result, raw, err := client.SendPrepared(t.Context(), prepare(t, onesie.TypeSafe(), client, asked))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if noul, _ := result.Noul("q"); noul == nil || noul.Noul != 0.1 || string(raw) != shortAnswer {
			t.Errorf("result = %+v and body %s, want q at 0.1 and %s", result.Answers, raw, shortAnswer)
		}

		if got := transport.lastBody(); got != `{"model":"jev-latest",`+asked[1:] {
			t.Errorf("sent %s, want the prepared body", got)
		}
	})

	t.Run("should send a provider's own encoding and return the decoded body", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: `{"result":` + shortAnswer + `,"success":true}`}
		client, _ := newTestClient(t, "https://api.example", onesie.WithProvider(onesie.Cloudflare()),
			onesie.WithHTTPClient(&http.Client{Transport: transport}))

		_, raw, err := client.SendPrepared(t.Context(), prepare(t, onesie.Cloudflare(), client, `{"model":"clef",`+asked[1:]))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if transport.last().URL.Path != "/run" || string(raw) != shortAnswer {
			t.Errorf("sent to %s and got %s, want /run and %s", transport.last().URL.Path, raw, shortAnswer)
		}
	})

	for _, provider := range onesie.Providers() {
		t.Run("should return the same one line body SystemOneRaw returns on "+provider.Name(), func(t *testing.T) {
			t.Parallel()

			const spaced = "{\n  \"model\": \"m\",\n  \"answers\": {\"q\": {\"type\": \"noul\", \"noul\": 0.1}},\n  \"usage\": {}\n}\n"

			response := spaced
			if provider.Name() == "cloudflare" {
				response = `{"result":` + spaced + "}\n"
			}

			transport := &recordingTransport{body: response}
			client, _ := newTestClient(t, "https://api.example", onesie.WithProvider(provider),
				onesie.WithHTTPClient(&http.Client{Transport: transport}))

			_, prepared, err := client.SendPrepared(t.Context(), prepare(t, provider, client, asked))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			unchecked, err := client.SystemOneRaw(t.Context(), json.RawMessage(asked))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if string(prepared) != shortAnswer || !bytes.Equal(prepared, unchecked) {
				t.Errorf("SendPrepared returned %q and SystemOneRaw %q, want both %s", prepared, unchecked, shortAnswer)
			}
		})
	}

	t.Run("should send long context as it was prepared", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: shortAnswer}
		client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))

		if _, _, err := client.SendPrepared(t.Context(), prepare(t, onesie.TypeSafe(), client, asked, onesie.WithLongContext())); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got := transport.last().Header.Get(onesie.HeaderLongContext); got != "true" || strings.Contains(transport.lastBody(), "long_context") {
			t.Errorf("header = %q and body %s, want the header and no long_context member", got, transport.lastBody())
		}
	})

	refusals := []struct {
		name    string
		body    func(t *testing.T, client *onesie.Client) *onesie.PreparedBody
		opts    []onesie.RequestOption
		wantErr string
	}{
		{
			name:    "should refuse a nil body",
			body:    func(*testing.T, *onesie.Client) *onesie.PreparedBody { return nil },
			wantErr: "onesie: the prepared body is nil. Use Provider.PrepareRaw",
		},
		{
			name: "should refuse a body prepared for another provider",
			body: func(t *testing.T, client *onesie.Client) *onesie.PreparedBody {
				t.Helper()

				return prepare(t, onesie.OpenRouter(), client, asked)
			},
			wantErr: "onesie: the body was prepared for the openrouter provider, and this client sends to typesafe",
		},
		{
			name: "should refuse WithLongContext, since long context is fixed when the body is prepared",
			body: func(t *testing.T, client *onesie.Client) *onesie.PreparedBody {
				t.Helper()

				return prepare(t, onesie.TypeSafe(), client, asked)
			},
			opts:    []onesie.RequestOption{onesie.WithLongContext()},
			wantErr: "onesie: long context is fixed when the body is prepared. Pass WithLongContext to PrepareRaw",
		},
	}

	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			transport := &recordingTransport{body: shortAnswer}
			client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))

			_, _, err := client.SendPrepared(t.Context(), tc.body(t, client), tc.opts...)
			if !errors.Is(err, onesie.ErrValidation) || err.Error() != tc.wantErr {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}

			if len(transport.requests) != 0 {
				t.Errorf("sent %d requests, want none", len(transport.requests))
			}
		})
	}

	faults := []struct {
		name     string
		body     string
		response string
		wantErr  string
	}{
		{
			name: "should check the answers against the prepared questions", body: asked,
			response: `{"model":"m","answers":{"q":{"type":"noul","noul":1.5}},"usage":{"input_tokens":7,"output_tokens":3}}`,
			wantErr:  "onesie: 200 response could not be used: question 'q' answered 1.5, which lies outside [0,1]",
		},
		{
			name: "should refuse a response that does not answer a prepared question", body: asked,
			response: `{"model":"m","answers":{"v":{"type":"noul","noul":0.5}},"usage":{}}`,
			wantErr:  "onesie: 200 response could not be used: question 'q' has no answer",
		},
		{
			name: "should refuse a response with no answers", body: `{"state":"s"}`, response: `{"x":1}`,
			wantErr: "onesie: 200 response could not be used: the response is not a System One result, since it has no answers",
		},
		{
			name: "should refuse an answer that does not decode", body: asked, response: `{"answers":{"q":{"type":"noul"}}}`,
			wantErr: "onesie: 200 response could not be used: answer 'q' has no noul, or it is null",
		},
		{
			name: "should keep the base URL hint for a response that is not JSON", body: asked, response: "garbage",
			wantErr: "onesie: 200 response could not be used: the response is not JSON, check the base URL https://api.example/v1/systemone",
		},
	}

	for _, tc := range faults {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			transport := &recordingTransport{body: tc.response}
			client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))

			_, raw, err := client.SendPrepared(t.Context(), prepare(t, onesie.TypeSafe(), client, tc.body))

			var unusable *onesie.ResponseError
			if !errors.As(err, &unusable) || err.Error() != tc.wantErr || raw != nil {
				t.Fatalf("error = %v and body %s, want %q and no body", err, raw, tc.wantErr)
			}
		})
	}

	t.Run("should carry the usage of an answer that does not fit", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: `{"answers":{"q":{"type":"noul","noul":2}},"usage":{"input_tokens":10,"output_tokens":2}}`}
		client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))

		_, _, err := client.SendPrepared(t.Context(), prepare(t, onesie.TypeSafe(), client, asked))

		var unusable *onesie.ResponseError
		if !errors.As(err, &unusable) || unusable.Usage == nil || unusable.Usage.InputTokens != 10 {
			t.Fatalf("error = %v, want a ResponseError carrying 10 input tokens", err)
		}
	})

	t.Run("should check the answers of a body that repeats a question id", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: `{"model":"m","answers":{"b":{"type":"noul","noul":0.5}},"usage":{}}`}
		client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))

		body := `{"state":"s","questions":{"a":{"type":"noul"},"b":{"type":"noul"},"a":{"type":"noul"}}}`
		_, _, err := client.SendPrepared(t.Context(), prepare(t, onesie.TypeSafe(), client, body))

		if want := "onesie: 200 response could not be used: question 'a' has no answer"; err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}
	})

	t.Run("should never prepare a body whose questions do not read", func(t *testing.T) {
		t.Parallel()

		body := `{"state":"s","questions":{"q":{"type":"tarot"}}}`

		_, err := onesie.TypeSafe().PrepareRaw(json.RawMessage(body), "")

		var refused *onesie.ValidationError
		if !errors.As(err, &refused) || refused.Question != "q" {
			t.Fatalf("error = %v, want a ValidationError naming q", err)
		}
	})
}

func TestClientProvider(t *testing.T) {
	t.Parallel()

	t.Run("should return the provider the client talks to", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestClient(t, "https://api.example", onesie.WithProvider(onesie.Levanto()))

		if got := client.Provider().Name(); got != "levanto" {
			t.Errorf("Provider().Name() = %q, want levanto", got)
		}
	})
}

func TestClientDefaultModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []onesie.Option
		env  map[string]string
		want string
	}{
		{name: "should return the provider's default", want: "jev-latest"},
		{name: "should return the option's model", opts: []onesie.Option{onesie.WithDefaultModel("m")}, want: "m"},
		{name: "should return the environment's model", env: map[string]string{"TYPESAFE_DEFAULT_MODEL": "e"}, want: "e"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			opts := append([]onesie.Option{onesie.WithAPIKey("k"), onesie.WithEnv(lookupFrom(tc.env))}, tc.opts...)

			client, err := onesie.New(opts...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := client.DefaultModel(); got != tc.want {
				t.Errorf("DefaultModel() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClientBaseURLSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []onesie.Option
		want onesie.BaseURLSource
	}{
		{name: "should report the default", want: onesie.BaseURLFromDefault},
		{name: "should report an option", opts: []onesie.Option{onesie.WithBaseURL("https://proxy.example")}, want: onesie.BaseURLFromOption},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, err := onesie.New(append([]onesie.Option{onesie.WithEnv(lookupFrom(nil)), onesie.WithAPIKey("k")}, tc.opts...)...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := client.BaseURLSource(); got != tc.want {
				t.Errorf("BaseURLSource() = %v, want %v", got, tc.want)
			}
		})
	}
}

func newTestClient(t *testing.T, url string, opts ...onesie.Option) (*onesie.Client, *mockClock) {
	t.Helper()

	clock := &mockClock{now: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}

	base := []onesie.Option{
		onesie.WithEnv(func(string) (string, bool) { return "", false }),
		onesie.WithAPIKey("sk-test"),
		onesie.WithBaseURL(url),
		onesie.WithClock(clock),
		onesie.WithRandom(func() float64 { return 0 }),
	}

	client, err := onesie.New(append(base, opts...)...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return client, clock
}

type mockClock struct {
	mu    sync.Mutex // The concurrency test drives one client from many goroutines.
	now   time.Time
	slept []time.Duration
}

func (c *mockClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *mockClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.slept = append(c.slept, d)
	c.now = c.now.Add(d)

	return nil
}

func (c *mockClock) Slept() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]time.Duration(nil), c.slept...)
}

func TestMarshalBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  onesie.Request
		want string
	}{
		{
			name: "should encode state model and questions in that order",
			req: onesie.Request{
				State: "the server is down",
				Model: "onesie-1.13.0",
				Questions: onesie.Questions{
					{ID: "urgent", Question: onesie.Noul{Instructions: "q"}},
				},
			},
			want: `{"state":"the server is down","model":"onesie-1.13.0","questions":` +
				`{"urgent":{"type":"noul","instructions":"q"}}}`,
		},
		{
			name: "should keep a raw state's key order and its digits",
			req: onesie.Request{
				State: json.RawMessage(`{"ticket_id":12345678901234567890,"zebra":1}`),
				Model: "onesie-1.13.0",
				Questions: onesie.Questions{
					{ID: "a", Question: onesie.Noul{Instructions: "q"}},
				},
			},
			want: `{"state":{"ticket_id":12345678901234567890,"zebra":1},` +
				`"model":"onesie-1.13.0","questions":` +
				`{"a":{"type":"noul","instructions":"q"}}}`,
		},
		{
			name: "should write assets after the questions",
			req: onesie.Request{
				State: map[string]any{"photo": "image-1"},
				Model: "m",
				Questions: onesie.Questions{
					{ID: "q", Question: onesie.Noul{Instructions: "a dog in `photo`"}},
				},
				Assets: onesie.Assets{"image-1": {Format: "png", Data: []byte{1}}},
			},
			want: `{"state":{"photo":"image-1"},"model":"m","questions":` +
				`{"q":{"type":"noul","instructions":"a dog in ` + "`photo`" + `"}},` +
				`"assets":{"image-1":{"type":"image","format":"png","data_base64":"AQ=="}}}`,
		},
		{
			name: "should leave out an empty assets map",
			req: onesie.Request{
				State:     "s",
				Model:     "m",
				Questions: onesie.Questions{{ID: "q", Question: onesie.Noul{Instructions: "q"}}},
				Assets:    onesie.Assets{},
			},
			want: `{"state":"s","model":"m","questions":{"q":{"type":"noul","instructions":"q"}}}`,
		},
		{
			name: "should write long_context after the questions when it is on",
			req: onesie.Request{
				State: "s", Model: "m", LongContext: true,
				Questions: onesie.Questions{{ID: "q", Question: onesie.Noul{Instructions: "q"}}},
			},
			want: `{"state":"s","model":"m","questions":{"q":{"type":"noul","instructions":"q"}},"long_context":true}`,
		},
		{
			name: "should keep the questions in slice order",
			req: onesie.Request{
				State: "s",
				Model: "m",
				Questions: onesie.Questions{
					{ID: "zebra", Question: onesie.Noul{Instructions: "z"}},
					{ID: "alpha", Question: onesie.Noul{Instructions: "a"}},
				},
			},
			want: `{"state":"s","model":"m","questions":` +
				`{"zebra":{"type":"noul","instructions":"z"},` +
				`"alpha":{"type":"noul","instructions":"a"}}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := onesie.MarshalBody(tc.req)
			if err != nil {
				t.Fatalf("MarshalBody: %v", err)
			}

			if string(got) != tc.want {
				t.Errorf("MarshalBody = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestMarshalQuestionsBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  onesie.Request
		want string
	}{
		{
			name: "should write the model and questions and leave out the state and assets",
			req: onesie.Request{
				State: "s", Model: "m", Questions: oneNoul(),
				Assets: onesie.Assets{"r": {Format: "png", Data: []byte{1}}},
			},
			want: `{"model":"m","questions":{"q":{"type":"noul","instructions":"Urgent?"}}}`,
		},
		{
			name: "should write long_context after the questions when it is on",
			req:  onesie.Request{Model: "m", Questions: oneNoul(), LongContext: true},
			want: `{"model":"m","questions":{"q":{"type":"noul","instructions":"Urgent?"}},"long_context":true}`,
		},
		{
			name: "should write a blank model and no questions as they are",
			req:  onesie.Request{},
			want: `{"model":"","questions":{}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := onesie.MarshalQuestionsBody(tc.req)
			if err != nil || string(got) != tc.want {
				t.Errorf("MarshalQuestionsBody = %s, %v, want %s", got, err, tc.want)
			}
		})
	}

	t.Run("should fail when a question cannot be marshalled", func(t *testing.T) {
		t.Parallel()

		req := onesie.Request{Questions: onesie.Questions{{ID: "c", Question: onesie.Choice{
			Criteria: onesie.Criteria{{Name: "a", Desc: make(chan int)}},
		}}}}

		if _, err := onesie.MarshalQuestionsBody(req); err == nil || !strings.Contains(err.Error(), "unsupported type: chan int") {
			t.Fatalf("error = %v, want an unsupported type", err)
		}
	})
}

func TestProviderResolveModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		model string
		env   map[string]string
		want  string
	}{
		{
			name:  "should prefer an explicit model",
			model: "onesie-1.9.9",
			env:   map[string]string{onesie.TypeSafe().EnvDefaultModel(): "onesie-1.2.0"},
			want:  "onesie-1.9.9",
		},
		{
			name: "should fall back to the environment",
			env:  map[string]string{onesie.TypeSafe().EnvDefaultModel(): "onesie-1.2.0"},
			want: "onesie-1.2.0",
		},
		{
			name: "should fall back to the built in default",
			want: onesie.TypeSafe().DefaultModel(),
		},
		{
			name:  "should trim an explicit model",
			model: " onesie-1.13.0 ",
			want:  "onesie-1.13.0",
		},
		{
			name:  "should treat a whitespace only model as absent",
			model: "   ",
			want:  onesie.TypeSafe().DefaultModel(),
		},
		{
			name:  "should treat a tab only model as absent",
			model: "\t",
			want:  onesie.TypeSafe().DefaultModel(),
		},
		{
			name:  "should fall back to the environment for a whitespace only model",
			model: "   ",
			env:   map[string]string{onesie.TypeSafe().EnvDefaultModel(): "onesie-1.2.0"},
			want:  "onesie-1.2.0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := onesie.TypeSafe().ResolveModel(tc.model, lookupFrom(tc.env)); got != tc.want {
				t.Errorf("ResolveModel = %s, want %s", got, tc.want)
			}
		})
	}

	t.Run("should default to systemone and ignore TYPESAFE_DEFAULT_MODEL under berget", func(t *testing.T) {
		t.Parallel()

		env := lookupFrom(map[string]string{onesie.TypeSafe().EnvDefaultModel(): "onesie-1.2.0"})
		if got := onesie.Berget().ResolveModel("", env); got != "systemone" {
			t.Errorf("ResolveModel = %s, want systemone", got)
		}

		if got := onesie.Berget().ResolveModel("laya-latest", env); got != "laya-latest" {
			t.Errorf("ResolveModel = %s, want laya-latest", got)
		}
	})

	t.Run("should default to sage-latest and ignore TYPESAFE_DEFAULT_MODEL under levanto", func(t *testing.T) {
		t.Parallel()

		env := lookupFrom(map[string]string{onesie.TypeSafe().EnvDefaultModel(): "onesie-1.2.0"})
		if got := onesie.Levanto().ResolveModel("", env); got != "sage-latest" {
			t.Errorf("ResolveModel = %s, want sage-latest", got)
		}

		if got := onesie.Levanto().ResolveModel("levanto-sage-v1.4", env); got != "levanto-sage-v1.4" {
			t.Errorf("ResolveModel = %s, want levanto-sage-v1.4", got)
		}
	})

	t.Run("should ignore TYPESAFE_DEFAULT_MODEL under openrouter", func(t *testing.T) {
		t.Parallel()

		env := lookupFrom(map[string]string{onesie.TypeSafe().EnvDefaultModel(): "onesie-1.2.0"})
		if got := onesie.OpenRouter().ResolveModel("", env); got != onesie.TypeSafe().DefaultModel() {
			t.Errorf("ResolveModel = %s, want %s", got, onesie.TypeSafe().DefaultModel())
		}
	})

	// A client resolving the model its own way is the drift this guards against, so every case is
	// asserted twice: once against ResolveModel and once against the body a client really sends.
	for _, tc := range tests {
		t.Run(tc.name+" in a sent body", func(t *testing.T) {
			t.Parallel()

			// The whole body rather than its model field, so a client that grew its own encoder
			// instead of calling MarshalBody fails here too.
			want, marshalErr := onesie.MarshalBody(onesie.Request{
				State: "s", Model: tc.want, Questions: oneNoul(),
			})
			if marshalErr != nil {
				t.Fatalf("MarshalBody: %v", marshalErr)
			}

			var (
				mu  sync.Mutex
				raw []byte
			)

			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					read, readErr := io.ReadAll(r.Body)
					if readErr != nil {
						t.Errorf("reading the request body: %v", readErr)
					}

					mu.Lock()
					raw = read
					mu.Unlock()

					w.Header().Set("Content-Type", "application/json")

					if _, err := io.WriteString(w, shortAnswer); err != nil {
						t.Errorf("writing the response: %v", err)
					}
				}))
			defer server.Close()

			client, err := onesie.New(onesie.WithEnv(lookupFrom(tc.env)),
				onesie.WithAPIKey("sk-test"), onesie.WithBaseURL(server.URL))
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			if _, err := client.SystemOne(t.Context(), onesie.Request{
				State: "s", Model: tc.model, Questions: oneNoul(),
			}); err != nil {
				t.Fatalf("SystemOne: %v", err)
			}

			mu.Lock()
			defer mu.Unlock()

			if string(raw) != string(want) {
				t.Errorf("sent body = %s, want %s", raw, want)
			}
		})
	}
}

func oneNoul() onesie.Questions {
	return onesie.Questions{{ID: "q", Question: onesie.Noul{Instructions: "Urgent?"}}}
}

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := env[name]

		return value, ok
	}
}

type recordingTransport struct {
	body string

	mu       sync.Mutex
	requests []*http.Request
	bodies   []string
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}

		body = string(raw)
	}

	rt.mu.Lock()
	rt.requests = append(rt.requests, req)
	rt.bodies = append(rt.bodies, body)
	rt.mu.Unlock()

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(rt.body)),
		Request:    req,
	}, nil
}

func (rt *recordingTransport) last() *http.Request {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	return rt.requests[len(rt.requests)-1]
}

func (rt *recordingTransport) lastBody() string {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	return rt.bodies[len(rt.bodies)-1]
}

type netError struct {
	timeout bool
}

func (e netError) Error() string {
	return "network failed"
}

func (e netError) Timeout() bool {
	return e.timeout
}

func (e netError) Temporary() bool {
	return false
}

type stallingTransport struct {
	unavailableFirst bool
	calls            atomic.Int32
}

func (s *stallingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if s.calls.Add(1) == 1 && s.unavailableFirst {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       http.NoBody,
			Request:    req,
		}, nil
	}

	<-req.Context().Done()

	return nil, req.Context().Err()
}

func newScriptedClient(
	t *testing.T, replies []scriptedReply, opts ...onesie.Option,
) (*onesie.Client, *mockClock, *scriptedTransport) {
	t.Helper()

	transport := &scriptedTransport{replies: replies}
	client, clock := newTestClient(t, "https://api.example", append([]onesie.Option{
		onesie.WithHTTPClient(&http.Client{Transport: transport}),
		onesie.WithAttemptObserver(transport.observe),
	}, opts...)...)

	return client, clock, transport
}

type scriptedReply struct {
	status int
	header http.Header
	body   string
	err    error
}

type scriptedTransport struct {
	replies []scriptedReply

	mu       sync.Mutex
	requests []*http.Request
	seen     []onesie.Attempt
}

func (s *scriptedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		if _, err := io.Copy(io.Discard, req.Body); err != nil {
			return nil, err
		}
	}

	s.mu.Lock()
	s.requests = append(s.requests, req)
	reply := s.replies[min(len(s.requests), len(s.replies))-1]
	s.mu.Unlock()

	if reply.err != nil {
		return nil, reply.err
	}

	header := reply.header.Clone()
	if header == nil {
		header = http.Header{}
	}

	return &http.Response{
		StatusCode: reply.status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(reply.body)),
		Request:    req,
	}, nil
}

func (s *scriptedTransport) observe(attempt onesie.Attempt) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seen = append(s.seen, attempt)
}

func (s *scriptedTransport) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.requests)
}

func (s *scriptedTransport) retryCounts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	counts := make([]string, 0, len(s.requests))
	for _, req := range s.requests {
		counts = append(counts, req.Header.Get("X-TypeSafe-Retry-Count"))
	}

	return counts
}

func (s *scriptedTransport) attempts() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.seen) == 0 {
		return "no attempt was observed"
	}

	return fmt.Sprintf("first attempt status %d, error %v, of %d attempts", s.seen[0].Status, s.seen[0].Err, len(s.seen))
}
