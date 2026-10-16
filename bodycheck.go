package onesie

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

func checkBodyImages(body []byte) error {
	return validationOnly(scanBodyImages(body))
}

func validationOnly(err error) error {
	// A body that does not parse is forwarded, so the server reports it as it does today.
	var validation *ValidationError
	if errors.As(err, &validation) {
		return err
	}

	return nil
}

func scanBodyImages(body []byte) error {
	dec := jsontext.NewDecoder(bytes.NewReader(body), rawOptions...)
	if _, err := dec.ReadToken(); err != nil {
		return err
	}

	var (
		faults map[string]*ValidationError
		texts  []string
	)

	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return err
		}

		switch name.String() {
		case "assets":
			faults, err = assetFaults(dec)
		case "state":
			err = collectTexts(dec, &texts)
		case "questions":
			err = questionTexts(dec, &texts)
		default:
			err = dec.SkipValue()
		}

		if err != nil {
			return err
		}
	}

	refs := slices.Sorted(maps.Keys(faults))

	for _, ref := range refs {
		if !slices.ContainsFunc(texts, func(text string) bool { return strings.Contains(text, ref) }) {
			return &ValidationError{Message: fmt.Sprintf(
				"asset '%s' is not referenced in the state or the questions, and the API refuses that", ref)}
		}
	}

	for _, ref := range refs {
		if faults[ref] != nil {
			return faults[ref]
		}
	}

	return nil
}

func assetFaults(dec *jsontext.Decoder) (map[string]*ValidationError, error) {
	if dec.PeekKind() != '{' {
		return nil, &ValidationError{Message: "assets must be an object of images"}
	}

	if _, err := dec.ReadToken(); err != nil {
		return nil, err
	}

	faults := map[string]*ValidationError{}

	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return nil, err
		}

		ref := name.String()

		fault, err := assetFault(dec, ref)
		if err != nil {
			return nil, err
		}

		faults[ref] = fault
	}

	_, err := dec.ReadToken()

	return faults, err
}

func assetFault(dec *jsontext.Decoder, ref string) (*ValidationError, error) {
	if dec.PeekKind() != '{' {
		return nil, &ValidationError{Message: "assets must be an object of images"}
	}

	if _, err := dec.ReadToken(); err != nil {
		return nil, err
	}

	format, hasData := "", false

	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return nil, err
		}

		switch name.String() {
		case "format":
			if dec.PeekKind() == '"' {
				value, readErr := dec.ReadToken()
				if readErr != nil {
					return nil, readErr
				}

				format = value.String()

				continue
			}
		case "data_base64":
			hasData = dec.PeekKind() == '"'
		}

		// Skipped rather than read, so no image is copied.
		if err := dec.SkipValue(); err != nil {
			return nil, err
		}
	}

	if _, err := dec.ReadToken(); err != nil {
		return nil, err
	}

	if wire := wireFormat(format); wire != "png" && wire != "jpg" {
		return &ValidationError{Message: fmt.Sprintf(
			"image '%s' has format '%s', and an image must be png or jpg", ref, format)}, nil
	}

	if !hasData {
		return &ValidationError{Message: fmt.Sprintf("asset '%s' needs data_base64 as a string", ref)}, nil
	}

	return nil, nil
}

func questionTexts(dec *jsontext.Decoder, texts *[]string) error {
	if dec.PeekKind() != '{' {
		return collectTexts(dec, texts)
	}

	if _, err := dec.ReadToken(); err != nil {
		return err
	}

	for dec.PeekKind() != '}' {
		if _, err := dec.ReadToken(); err != nil {
			return err
		}

		if err := questionMembers(dec, texts); err != nil {
			return err
		}
	}

	_, err := dec.ReadToken()

	return err
}

func questionMembers(dec *jsontext.Decoder, texts *[]string) error {
	if dec.PeekKind() != '{' {
		return collectTexts(dec, texts)
	}

	if _, err := dec.ReadToken(); err != nil {
		return err
	}

	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return err
		}

		// The type is onesie's own wire field, not text the API replaces references in.
		if name.String() == "type" {
			if err := dec.SkipValue(); err != nil {
				return err
			}

			continue
		}

		if err := collectTexts(dec, texts); err != nil {
			return err
		}
	}

	_, err := dec.ReadToken()

	return err
}

func collectTexts(dec *jsontext.Decoder, texts *[]string) error {
	switch dec.PeekKind() {
	case '{':
		if _, err := dec.ReadToken(); err != nil {
			return err
		}

		for dec.PeekKind() != '}' {
			if _, err := dec.ReadToken(); err != nil {
				return err
			}

			if err := collectTexts(dec, texts); err != nil {
				return err
			}
		}

		_, err := dec.ReadToken()

		return err
	case '[':
		if _, err := dec.ReadToken(); err != nil {
			return err
		}

		for dec.PeekKind() != ']' {
			if err := collectTexts(dec, texts); err != nil {
				return err
			}
		}

		_, err := dec.ReadToken()

		return err
	case '"':
		value, err := dec.ReadToken()
		if err != nil {
			return err
		}

		*texts = append(*texts, value.String())

		return nil
	default:
		return dec.SkipValue()
	}
}
