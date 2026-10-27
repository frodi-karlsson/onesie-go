package onesie_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/frodi-karlsson/onesie-go"
)

func TestValidateBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{name: "should accept an https URL", raw: "https://api.example"},
		{name: "should accept an http URL with a port and a path", raw: "http://localhost:8080/proxy"},
		{
			name:    "should refuse another scheme",
			raw:     "ftp://api.example",
			wantErr: "onesie: base URL ftp://api.example is not an http or https URL",
		},
		{
			name:    "should refuse a URL with no scheme",
			raw:     "api.example",
			wantErr: "onesie: base URL api.example is not an http or https URL",
		},
		{
			name:    "should refuse a URL that names no host",
			raw:     "https://",
			wantErr: "onesie: base URL https:// names no host. Write it as https://HOST",
		},
		{
			name:    "should refuse a URL that names no host before its path, echoing it as given",
			raw:     "http:///v1/",
			wantErr: "onesie: base URL http:///v1/ names no host. Write it as http://HOST",
		},
		{
			name:    "should refuse a URL that does not parse, with the reason",
			raw:     "https://api.example:port",
			wantErr: `onesie: base URL https://api.example:port does not parse: invalid port ":port" after host`,
		},
		{
			name:    "should leave the userinfo out of the echo",
			raw:     "ftp://user:secret@api.example",
			wantErr: "onesie: base URL ftp://api.example is not an http or https URL",
		},
		{
			name:    "should leave the reason out when it could hold part of the userinfo",
			raw:     "https://user:secret@api.example:port",
			wantErr: "onesie: base URL https://api.example:port does not parse",
		},
		{
			name:    "should echo no URL when the last @ comes after the authority",
			raw:     "https://user:sec/ret@api.example",
			wantErr: "onesie: the base URL does not parse",
		},
		{
			name:    "should echo no URL for an @ in the query",
			raw:     "ftp://host/p?q=a@b",
			wantErr: "onesie: the base URL is not an http or https URL",
		},
		{
			name:    "should echo no URL for userinfo ahead of the scheme",
			raw:     "user:secret@https://host",
			wantErr: "onesie: the base URL is not an http or https URL",
		},
		{
			name:    "should echo no URL for an @ in the scheme",
			raw:     "user@x://user:secret@host",
			wantErr: "onesie: the base URL does not parse",
		},
		{
			name:    "should echo no URL for userinfo with no scheme",
			raw:     "user:secret@api.example",
			wantErr: "onesie: the base URL is not an http or https URL",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := onesie.ValidateBaseURL(tc.raw)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}

			if !errors.Is(err, onesie.ErrValidation) || err.Error() != tc.wantErr {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}

			if strings.Contains(err.Error(), "user") || strings.Contains(err.Error(), "sec") {
				t.Errorf("error = %q, want no userinfo", err.Error())
			}
		})
	}
}
