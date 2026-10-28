package onesie

import (
	"errors"
	"net"
	"strings"
)

func (e *ConnectionError) cause(err error) string {
	if e.certificate != "" {
		return e.certificate
	}

	return causeOf(err)
}

func throughProxy(err error) string {
	var dial *net.OpError
	for errors.As(err, &dial) {
		if dial.Op == "proxyconnect" {
			return " through a proxy"
		}

		err = dial.Err
	}

	return ""
}

func causeOf(err error) string {
	if problem := certificateProblem(err); problem != "" {
		return problem
	}

	for next := errors.Unwrap(err); next != nil; next = errors.Unwrap(err) {
		err = next
	}

	// The text after the last colon, since the net package writes the address it dialled or the
	// name it looked up before one.
	text, _, _ := strings.Cut(err.Error(), "\n")
	if at := strings.LastIndex(text, ": "); at >= 0 {
		text = text[at+len(": "):]
	}

	if shown := withoutAddresses(firstLine(text)); shown != "" {
		return shown
	}

	return "connection failed"
}

func certificateProblem(err error) string {
	for ; err != nil; err = errors.Unwrap(err) {
		if text, isCertificate := strings.CutPrefix(err.Error(), "x509: "); isCertificate {
			problem, _, _ := strings.Cut(firstLine(text), ": ")
			problem, _, _ = strings.Cut(problem, " (")

			return withoutAddresses(problem)
		}
	}

	return ""
}

func withoutAddresses(text string) string {
	kept := make([]string, 0, strings.Count(text, " ")+1)

	for _, token := range strings.Fields(text) {
		if !strings.Contains(token, "://") && !strings.Contains(token, "@") {
			kept = append(kept, token)
		}
	}

	return strings.Join(kept, " ")
}
