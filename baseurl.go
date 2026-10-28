package onesie

import (
	"net"
	"net/url"
	"strings"
)

const (
	ruleUnsafe   = "must not hold @, ? or #"
	ruleUnparsed = "does not parse"
	ruleScheme   = "is not an http or https URL"
	ruleNoHost   = "names no host"
	ruleInsecure = "is plain http to a host other than this machine"
)

const (
	// BaseURLSourceUnknown is the source of a URL checked with no context, as ValidateBaseURL does.
	BaseURLSourceUnknown BaseURLSource = iota
	// BaseURLFromOption is a URL passed to WithBaseURL or as ResolveBaseURL's explicit value.
	BaseURLFromOption
	// BaseURLFromEnv is a URL read from the provider's EnvBaseURL variable.
	BaseURLFromEnv
	// BaseURLFromAccount is a URL built from an account id, passed in or read from EnvAccountID.
	BaseURLFromAccount
	// BaseURLFromDefault is the provider's DefaultBaseURL.
	BaseURLFromDefault
)

// ValidateBaseURL checks that raw is an absolute http or https URL with a host, holding no userinfo,
// query or fragment marker. A refusal is a *BaseURLError whose Source is BaseURLSourceUnknown.
func ValidateBaseURL(raw string) error {
	if refused := checkBaseURL(raw, BaseURLSourceUnknown); refused != nil {
		return refused
	}

	return nil
}

func checkBaseURL(raw string, source BaseURLSource) *BaseURLError {
	// Checked before parsing, since url.Parse reads a password holding a slash as a host and a path.
	if strings.ContainsAny(raw, "@?#") {
		return newBaseURLError("", source, ruleUnsafe, userInfoKind(raw))
	}

	parsed, err := url.Parse(raw)

	switch {
	case err != nil:
		return newBaseURLError("", source, ruleUnparsed, nil)
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		return newBaseURLError(shownOf(parsed), source, ruleScheme, nil)
	case parsed.Host == "":
		return newBaseURLError(shownOf(parsed), source, ruleNoHost, nil)
	default:
		return nil
	}
}

func userInfoKind(raw string) error {
	if strings.Contains(raw, "@") {
		return ErrBaseURLUserInfo
	}

	return nil
}

func shownOf(parsed *url.URL) string {
	if parsed.Scheme == "" {
		return ""
	}

	return parsed.Scheme + "://" + parsed.Host
}

func newBaseURLError(shown string, source BaseURLSource, rule string, kind error) *BaseURLError {
	refused := &BaseURLError{Source: source, Shown: shown, rule: rule}
	refused.ValidationError = ValidationError{Message: refused.Reason(), kind: kind}

	return refused
}

// InsecureBaseURL reports whether raw is plain http to a host other than this machine. Only
// localhost and loopback addresses count as this machine.
func InsecureBaseURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" {
		return false
	}

	// localhost with a trailing dot counts as remote, since a resolver may send it on to DNS rather
	// than answer it from the hosts file.
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return false
	}

	ip := net.ParseIP(host)

	return ip == nil || !ip.IsLoopback()
}

func refuseInsecure(raw string, source BaseURLSource) *BaseURLError {
	parsed, err := url.Parse(raw)
	if err != nil {
		return newBaseURLError("", source, ruleInsecure, ErrInsecureBaseURL)
	}

	return newBaseURLError(shownOf(parsed), source, ruleInsecure, ErrInsecureBaseURL)
}

// BaseURLSource is one of the BaseURLFrom constants, or BaseURLSourceUnknown.
type BaseURLSource int

// BaseURLError is a refused base URL. Shown is its scheme and host when they are safe to show, and
// Source says where it came from.
type BaseURLError struct {
	ValidationError

	Source BaseURLSource
	Shown  string

	rule string
}

// Rule is the rule the base URL broke, without the URL.
func (e *BaseURLError) Rule() string {
	return e.rule
}

// Reason names the base URL when it is safe to, and the rule it broke.
func (e *BaseURLError) Reason() string {
	if e.Shown == "" {
		return "the base URL " + e.rule
	}

	return "base URL " + e.Shown + " " + e.rule
}

// Error is Reason with the package prefix.
func (e *BaseURLError) Error() string {
	return "onesie: " + e.Reason()
}

// Unwrap returns the embedded ValidationError, so errors.As reaches it.
func (e *BaseURLError) Unwrap() error {
	return &e.ValidationError
}
