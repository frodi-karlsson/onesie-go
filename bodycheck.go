package onesie

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"maps"
	"slices"
)

func checkBodyImages(members []member) error {
	return validationOnly(scanBodyImages(members))
}

func validationOnly(err error) error {
	// A body the scan cannot read is forwarded, so the server reports it.
	var validation *ValidationError
	if errors.As(err, &validation) {
		return err
	}

	return nil
}

func scanBodyImages(members []member) error {
	var (
		faults map[string]*ValidationError
		texts  []string
	)

	for _, m := range members {
		var err error

		if m.name == "assets" {
			faults, err = assetFaults(m.value)
		} else {
			err = referenceTexts(m, &texts)
		}

		if err != nil {
			return err
		}
	}

	refs := slices.Sorted(maps.Keys(faults))

	if missing := unreferenced(texts, refs); len(missing) > 0 {
		return notReferenced(missing[0])
	}

	for _, ref := range refs {
		if faults[ref] != nil {
			return faults[ref]
		}
	}

	return nil
}

func referenceTexts(m member, texts *[]string) error {
	dec := jsontext.NewDecoder(bytes.NewBuffer(m.value), rawOptions()...)

	switch m.name {
	case "state":
		return collectTexts(dec, texts)
	case "questions":
		return questionTexts(dec, texts)
	default:
		return nil
	}
}

func assetFaults(value jsontext.Value) (map[string]*ValidationError, error) {
	assets, err := readAssets(value)
	if err != nil {
		return nil, err
	}

	faults := make(map[string]*ValidationError, len(assets))
	for ref, asset := range assets {
		faults[ref] = asset.fault(ref)
	}

	return faults, nil
}

func readAssets(value jsontext.Value) (map[string]rawAsset, error) {
	// A bytes.Buffer, which jsontext parses in place, so no image is copied.
	dec := jsontext.NewDecoder(bytes.NewBuffer(value), rawOptions()...)
	if dec.PeekKind() != '{' {
		return nil, &ValidationError{Message: "assets must be an object of images"}
	}

	if _, err := dec.ReadToken(); err != nil {
		return nil, err
	}

	assets := map[string]rawAsset{}

	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return nil, err
		}

		ref := name.String()

		asset, err := readAsset(dec, value)
		if err != nil {
			return nil, err
		}

		assets[ref] = asset
	}

	_, err := dec.ReadToken()

	return assets, err
}

func readAsset(dec *jsontext.Decoder, value []byte) (rawAsset, error) {
	if dec.PeekKind() != '{' {
		return rawAsset{}, &ValidationError{Message: "assets must be an object of images"}
	}

	if _, err := dec.ReadToken(); err != nil {
		return rawAsset{}, err
	}

	var asset rawAsset

	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return rawAsset{}, err
		}

		field, isText, start := name.String(), dec.PeekKind() == '"', dec.InputOffset()

		if err = dec.SkipValue(); err != nil {
			return rawAsset{}, err
		}

		// Matched exactly, and the last of a repeated name wins, as the API reads a body.
		switch raw := valueAt(value, start, dec.InputOffset()); field {
		case "format":
			asset.format = ""
			if isText {
				asset.format, err = textOf(raw)
			}
		case "data_base64":
			asset.data = nil
			if isText {
				asset.data = raw
			}
		}

		if err != nil {
			return rawAsset{}, err
		}
	}

	_, err := dec.ReadToken()

	return asset, err
}

type rawAsset struct {
	format string
	data   jsontext.Value
}

func (a rawAsset) fault(ref string) *ValidationError {
	if fault := checkFormat(ref, a.format); fault != nil {
		return fault
	}

	if a.data == nil {
		return &ValidationError{Message: fmt.Sprintf("asset '%s' needs data_base64 as a string", ref)}
	}

	return nil
}

func textOf(raw jsontext.Value) (string, error) {
	var text string
	err := json.Unmarshal(raw, &text)

	return text, err
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

		if holdsNoReferences(name.String()) {
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

func holdsNoReferences(field string) bool {
	// The type is onesie's own wire field, not text the API replaces references in.
	return field == "type"
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

func checkQuestionCounts(members []member) error {
	for _, m := range members {
		if m.name != "questions" {
			continue
		}

		var questions map[string]struct {
			Type     string          `json:"type"`
			Criteria json.RawMessage `json:"criteria"`
		}

		// A body whose questions do not decode is forwarded, so the server reports it.
		if json.Unmarshal(m.value, &questions) != nil {
			continue
		}

		for _, id := range slices.Sorted(maps.Keys(questions)) {
			if err := checkCount(id, questions[id].Type, questions[id].Criteria); err != nil {
				return err
			}
		}
	}

	return nil
}

func checkCount(id, kind string, criteria json.RawMessage) error {
	switch kind {
	case "choice":
		var options map[string]json.RawMessage
		if json.Unmarshal(criteria, &options) == nil && len(options) > MaxChoiceOptions {
			return tooManyOptions(id, len(options))
		}
	case "score":
		var levels []json.RawMessage
		if json.Unmarshal(criteria, &levels) == nil && len(levels) > MaxScoreLevels {
			return tooManyLevels(id, len(levels))
		}
	}

	return nil
}
