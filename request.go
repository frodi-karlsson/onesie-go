package onesie

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

const (
	maxReferenceBytes     = 1024
	maxAllReferencesBytes = 65536
)

// ValidateRequest refuses a request the API would refuse: its questions, its assets, then the size of
// a body with images, measured with the model as given rather than a client's default.
func ValidateRequest(req Request) error {
	if err := checkRequest(req); err != nil {
		return err
	}

	if len(req.Assets) == 0 {
		return nil
	}

	_, err := checkedBody(req, req.Model)

	return err
}

func checkRequest(req Request) error {
	if err := ValidateQuestions(req.Questions); err != nil {
		return err
	}

	if err := checkBoxes(req); err != nil {
		return err
	}

	if err := checkReferenceNames(req.Assets); err != nil {
		return err
	}

	for _, ref := range slices.Sorted(maps.Keys(req.Assets)) {
		if !req.Assets[ref].built() {
			return &ValidationError{Message: fmt.Sprintf("image '%s' was not built with NewImage", printable(ref))}
		}
	}

	return nil
}

func checkedBody(req Request, model string) (jsonBody, error) {
	// Long context travels as a header, so the body never carries it.
	body, err := bodyOf(Request{State: req.State, Questions: req.Questions, Assets: req.Assets}, model, wholeBody)
	if err != nil || len(req.Assets) == 0 {
		return body, err
	}

	if err = checkReferenced(body.members, req.Assets); err != nil {
		return jsonBody{}, err
	}

	return body, checkBodySize(len(body.raw))
}

func checkBoxes(req Request) error {
	if len(req.Assets) > 0 {
		return nil
	}

	for _, named := range req.Questions {
		if _, isBox := dereferenced(named.Question).(Box); isBox {
			return &ValidationError{
				Question: named.ID,
				Message:  fmt.Sprintf("box question '%s' needs an image in the request", printable(named.ID)),
			}
		}
	}

	return nil
}

func checkReferenceNames(assets Assets) error {
	refs := slices.Sorted(maps.Keys(assets))
	total := 0

	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			return &ValidationError{Message: "an asset reference must not be blank"}
		}

		if len(ref) > maxReferenceBytes {
			return &ValidationError{Message: fmt.Sprintf(
				"asset reference '%.40s...' is %d bytes, and a reference is at most %d",
				printable(ref), len(ref), maxReferenceBytes)}
		}

		total += len(ref)
	}

	if total > maxAllReferencesBytes {
		return &ValidationError{Message: fmt.Sprintf(
			"the asset references are %d bytes together, and at most %d are allowed",
			total, maxAllReferencesBytes)}
	}

	return nil
}

func checkReferenced(members []member, assets Assets) error {
	texts, err := textsOf(members)
	if err != nil {
		return err
	}

	if missing := unreferenced(texts, slices.Sorted(maps.Keys(assets))); len(missing) > 0 {
		return notReferenced(missing[0])
	}

	return nil
}

// UnreferencedAssets lists, sorted, the references no string value of the state or questions names.
// At each place in a text the longest reference starting there counts, so one inside a longer one does not.
func UnreferencedAssets(req Request) ([]string, error) {
	// Through JSON, so a state of any Go type, a json.RawMessage included, is walked as a raw body is.
	body, err := bodyOf(Request{State: req.State, Questions: req.Questions}, "", wholeBody)
	if err != nil {
		return nil, err
	}

	texts, err := textsOf(body.members)
	if err != nil {
		return nil, err
	}

	return unreferenced(texts, slices.Sorted(maps.Keys(req.Assets))), nil
}

func textsOf(members []member) ([]string, error) {
	var texts []string

	for _, m := range members {
		if err := referenceTexts(m, &texts); err != nil {
			return nil, unencodable(err)
		}
	}

	return texts, nil
}

func unencodable(err error) error {
	return &ValidationError{Message: "the request cannot be encoded: " + encodeFault(err), cause: err}
}

func encodeFault(err error) string {
	// encoding/json/v2 words its own errors at random once a process, so the refusal is worded from
	// the fields it does promise. A syntactic error comes first, since a semantic one can wrap it.
	var (
		syntactic *jsontext.SyntacticError
		semantic  *jsonv2.SemanticError
	)

	switch {
	case errors.As(err, &syntactic) && syntactic.Err != nil:
		return fmt.Sprintf("%v at %s", syntactic.Err, place(syntactic.JSONPointer))
	case errors.As(err, &semantic) && semantic.Err != nil:
		return fmt.Sprintf("Go %v at %s: %v", semantic.GoType, place(semantic.JSONPointer), semantic.Err)
	case errors.As(err, &semantic):
		return fmt.Sprintf("Go %v at %s has no JSON encoding", semantic.GoType, place(semantic.JSONPointer))
	default:
		return err.Error()
	}
}

func place(pointer jsontext.Pointer) string {
	return printable(string(pointer))
}

func checkBodySize(size int) error {
	if size > MaxBodyBytes {
		return &ValidationError{Message: fmt.Sprintf(
			"the request body is %d bytes, and the API takes at most %d. Send fewer or smaller images",
			size, MaxBodyBytes)}
	}

	return nil
}
