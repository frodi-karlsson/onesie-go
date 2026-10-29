package onesie

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxBodyInError = 200
	byteOrderMark  = "\ufeff"

	zeroWidthNonJoiner = '\u200c'
	zeroWidthJoiner    = '\u200d'
)

func printable(text string) string {
	return truncate(withoutControls(text))
}

func capitalised(text string) string {
	return strings.ToUpper(text[:1]) + text[1:]
}

func firstLine(body string) string {
	line, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(body), byteOrderMark), "\n")

	return strings.TrimSpace(withoutControls(line))
}

func withoutControls(text string) string {
	return strings.Map(func(r rune) rune {
		if misusable(r) {
			return -1
		}

		return r
	}, text)
}

func misusable(r rune) bool {
	// Every format character goes, bidi controls and tag characters among them, since a terminal lets one
	// reorder or hide text. The two joiners stay, since they can do neither and emoji and scripts need them.
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) && r != zeroWidthJoiner && r != zeroWidthNonJoiner
}

func truncate(raw string) string {
	if len(raw) <= maxBodyInError {
		return raw
	}

	cut := maxBodyInError
	for cut > 0 && !utf8.RuneStart(raw[cut]) {
		cut--
	}

	return raw[:cut] + "..."
}
