package onesie

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestClientExchange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		maxBytes int64
		reply    func(*http.Request) (*http.Response, error)
		status   int
		body     string
		wantErr  string
	}{
		{
			name: "should return the status, header and body the server sent", maxBytes: 64, status: http.StatusTeapot, body: `{"a":1}`,
			reply: func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusTeapot, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"a":1}`)), Request: req}, nil
			},
		},
		{
			name: "should refuse a body over the limit", maxBytes: 3,
			reply: func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"a":1}`)), Request: req}, nil
			},
			wantErr: "onesie: connection error: response body exceeded 3 bytes",
		},
		{
			name: "should report a transport failure as a connection error", maxBytes: 64,
			reply: func(*http.Request) (*http.Response, error) {
				return nil, errors.New("connection reset")
			},
			wantErr: "onesie: could not reach api.example: connection reset",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := &Client{httpClient: &http.Client{Transport: roundTripper(tc.reply)}, maxResponseBytes: tc.maxBytes, clock: systemClock{}}

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.example/v1/models", http.NoBody)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got, err := client.exchange(t.Context(), t.Context(), "#1", req, requestConfig{})
			if tc.wantErr != "" {
				if !errors.Is(err, ErrConnection) || err.Error() != tc.wantErr || got != nil {
					t.Fatalf("exchange = %+v, %v, want %q", got, err, tc.wantErr)
				}

				return
			}

			if err != nil || got.status != tc.status || string(got.body) != tc.body {
				t.Errorf("exchange = %+v, %v, want %d and %s", got, err, tc.status, tc.body)
			}
		})
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
