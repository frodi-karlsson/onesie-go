package onesie

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestClientVerifyKey(t *testing.T) {
	t.Parallel()

	const (
		typesafeListing   = `{"models":[{"name":"jev-latest","description":"alias","release_date":"2026-08-01"}]}`
		openRouterListing = `{"data":[{"id":"typesafe/jev-latest","description":"Jev","created":1785542400}]}`
	)

	keyless := OpenRouter()
	keyless.keyCheck = keyCheck{}

	tests := []struct {
		name     string
		provider Provider
		listing  string
	}{
		{
			name: "should only list the models for a provider whose key check is the zero value",
			provider: Provider{
				name:         "custom",
				modelsPath:   "/v1/models",
				decodeModels: decodeTypeSafeModels,
				routeFor: oneRoute(route{
					path:   systemOnePath,
					takes:  takesEverything(),
					encode: encodeAsIs,
					decode: identity,
				}),
			},
			listing: typesafeListing,
		},
		{
			name:     "should only list the models for openrouter with its key check cleared",
			provider: keyless,
			listing:  openRouterListing,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var (
				mu    sync.Mutex
				paths []string
			)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				paths = append(paths, r.Method+" "+r.URL.Path)
				mu.Unlock()

				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.listing)
			}))
			defer server.Close()

			client, err := New(
				WithEnv(func(string) (string, bool) { return "", false }),
				WithAPIKey("sk-test"),
				WithBaseURL(server.URL),
				WithProvider(tc.provider),
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if _, err := client.VerifyKey(t.Context()); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			mu.Lock()
			defer mu.Unlock()

			if strings.Join(paths, ", ") != "GET /v1/models" {
				t.Errorf("requests got %v, want only GET /v1/models", paths)
			}
		})
	}
}

func TestDefaultHTTPClient(t *testing.T) {
	t.Parallel()

	t.Run("should keep enough idle connections for calls run at once, and leave Go's default transport alone", func(t *testing.T) {
		t.Parallel()

		client, err := New(WithAPIKey("k"), WithEnv(func(string) (string, bool) { return "", false }))
		if err != nil {
			t.Fatal(err)
		}

		transport, ok := client.httpClient.Transport.(*http.Transport)
		if !ok || transport.MaxIdleConnsPerHost != idleConnsPerHost {
			t.Errorf("transport = %#v, want MaxIdleConnsPerHost %d", client.httpClient.Transport, idleConnsPerHost)
		}

		if transport == http.DefaultTransport || http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost != 0 {
			t.Error("New changed http.DefaultTransport, want a clone")
		}
	})
}
