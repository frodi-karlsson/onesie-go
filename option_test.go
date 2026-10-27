package onesie_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/frodi-karlsson/onesie-go"
)

const longContextRefused = "onesie: the TypeSafe-Danger-Zone-Long-Context header is set by WithLongContext, not by hand"

func TestWithHeader(t *testing.T) {
	t.Parallel()

	for _, name := range []string{onesie.HeaderLongContext, "typesafe-danger-zone-long-context"} {
		t.Run("should refuse the long context header spelled "+name, func(t *testing.T) {
			t.Parallel()

			_, err := onesie.New(onesie.WithAPIKey("k"), onesie.WithHeader(name, "true"))
			if !errors.Is(err, onesie.ErrValidation) || err.Error() != longContextRefused {
				t.Fatalf("error = %v, want %q", err, longContextRefused)
			}
		})
	}
}

func TestWithRequestHeader(t *testing.T) {
	t.Parallel()

	t.Run("should refuse the long context header before any request", func(t *testing.T) {
		t.Parallel()

		transport := &recordingTransport{body: shortAnswer}
		client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))

		_, err := client.SystemOne(t.Context(), onesie.Request{State: "s", Questions: oneNoul()},
			onesie.WithRequestHeader("TYPESAFE-DANGER-ZONE-LONG-CONTEXT", "true"))
		if !errors.Is(err, onesie.ErrValidation) || err.Error() != longContextRefused {
			t.Fatalf("error = %v, want %q", err, longContextRefused)
		}

		if len(transport.requests) != 0 {
			t.Errorf("requests = %d, want none", len(transport.requests))
		}
	})
}

func TestWithLongContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  onesie.Request
		opts []onesie.RequestOption
	}{
		{name: "should send the header once for the option", req: onesie.Request{State: "s", Questions: oneNoul()}, opts: []onesie.RequestOption{onesie.WithLongContext()}},
		{
			name: "should send the header once when the request and the option both ask",
			req:  onesie.Request{State: "s", Questions: oneNoul(), LongContext: true},
			opts: []onesie.RequestOption{onesie.WithLongContext(), onesie.WithLongContext()},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			transport := &recordingTransport{body: shortAnswer}
			client, _ := newTestClient(t, "https://api.example", onesie.WithHTTPClient(&http.Client{Transport: transport}))

			if _, err := client.SystemOne(t.Context(), tc.req, tc.opts...); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got := transport.last().Header.Values(onesie.HeaderLongContext); len(got) != 1 || got[0] != "true" {
				t.Errorf("%s = %v, want one true", onesie.HeaderLongContext, got)
			}
		})
	}
}
