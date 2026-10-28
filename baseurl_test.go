package onesie_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/frodi-karlsson/onesie-go"
)

func TestValidateBaseURL(t *testing.T) {
	t.Parallel()

	const unsafe = "onesie: the base URL must not hold @, ? or #"

	tests := []struct {
		name     string
		raw      string
		wantErr  string
		shown    string
		rule     string
		secret   string
		userInfo bool
	}{
		{name: "should accept an https URL", raw: "https://api.example"},
		{name: "should accept an http URL with a port and a path", raw: "http://localhost:8080/proxy"},
		{name: "should accept plain http to another host, since it checks shape only", raw: "http://proxy.example"},
		{
			name: "should refuse another scheme", raw: "ftp://api.example",
			wantErr: "onesie: base URL ftp://api.example is not an http or https URL", shown: "ftp://api.example",
			rule: "is not an http or https URL",
		},
		{
			name: "should refuse a URL with no scheme without naming it", raw: "api.example",
			wantErr: "onesie: the base URL is not an http or https URL", rule: "is not an http or https URL",
		},
		{
			name: "should refuse a URL that names no host", raw: "https://",
			wantErr: "onesie: base URL https:// names no host", shown: "https://", rule: "names no host",
		},
		{
			name: "should show only the scheme and host of a URL with a path", raw: "http:///v1/",
			wantErr: "onesie: base URL http:// names no host", shown: "http://", rule: "names no host",
		},
		{
			name: "should refuse a URL that does not parse without quoting the parser", raw: "https://api.example:port",
			wantErr: "onesie: the base URL does not parse", rule: "does not parse",
		},
		{
			name: "should refuse userinfo", raw: "ftp://user:secret@api.example",
			wantErr: unsafe, rule: "must not hold @, ? or #", secret: "secret", userInfo: true,
		},
		{
			name: "should refuse a key that parses as a host", raw: "https://sk-secret/rest@host",
			wantErr: unsafe, rule: "must not hold @, ? or #", secret: "sk-secret", userInfo: true,
		},
		{
			name: "should refuse a password holding a slash", raw: "https://user:pa/ss@host",
			wantErr: unsafe, rule: "must not hold @, ? or #", secret: "pa", userInfo: true,
		},
		{
			name: "should refuse a query", raw: "https://api.example/v1?key=sk-secret",
			wantErr: unsafe, rule: "must not hold @, ? or #", secret: "sk-secret",
		},
		{
			name: "should refuse a fragment", raw: "https://api.example/#sk-secret",
			wantErr: unsafe, rule: "must not hold @, ? or #", secret: "sk-secret",
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

			var refused *onesie.BaseURLError
			if !errors.As(err, &refused) || !errors.Is(err, onesie.ErrValidation) || err.Error() != tc.wantErr {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}

			if refused.Reason() != strings.TrimPrefix(tc.wantErr, "onesie: ") || refused.Shown != tc.shown ||
				refused.Rule() != tc.rule || refused.Source != onesie.BaseURLSourceUnknown {
				t.Errorf("refusal = %+v with reason %q and rule %q", refused, refused.Reason(), refused.Rule())
			}

			if tc.secret != "" && (strings.Contains(err.Error(), tc.secret) || strings.Contains(refused.Reason(), tc.secret)) {
				t.Errorf("error = %q, want no %q", err.Error(), tc.secret)
			}

			if errors.Is(err, onesie.ErrInsecureBaseURL) {
				t.Errorf("a shape fault matched ErrInsecureBaseURL")
			}

			if errors.Is(err, onesie.ErrBaseURLUserInfo) != tc.userInfo {
				t.Errorf("errors.Is(err, ErrBaseURLUserInfo) = %v, want %v", !tc.userInfo, tc.userInfo)
			}
		})
	}
}

func TestInsecureBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "should flag plain http to another host", raw: "http://proxy.example", want: true},
		{name: "should flag localhost with a trailing dot", raw: "http://localhost./v1", want: true},
		{name: "should pass localhost", raw: "http://localhost:8080"},
		{name: "should pass localhost in any case", raw: "http://LocalHost"},
		{name: "should pass an IPv4 loopback address", raw: "http://127.1.2.3:9000"},
		{name: "should pass the IPv6 loopback address", raw: "http://[::1]:8080"},
		{name: "should pass https to another host", raw: "https://proxy.example"},
		{name: "should pass a URL that does not parse", raw: "http://[::1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := onesie.InsecureBaseURL(tc.raw); got != tc.want {
				t.Errorf("InsecureBaseURL(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
