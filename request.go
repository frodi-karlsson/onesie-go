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

	body, err := marshalRequest(req, req.Model)
	if err != nil {
		return err
	}

	return checkBodySize(len(body))
}

func checkRequest(req Request) error {
	if err := ValidateQuestions(req.Questions); err != nil {
		return err
	}

	if err := checkBoxes(req); err != nil {
		return err
	}

	if err := checkReferences(req); err != nil {
		return err
	}

	for _, ref := range slices.Sorted(maps.Keys(req.Assets)) {
		if err := ValidateImage(ref, req.Assets[ref]); err != nil {
			return err
		}
	}

	return nil
}

// ValidateImage refuses an image the API would refuse, naming it in the message. It checks the
// format, the file size and the sides, reading only the file's header.
func ValidateImage(name string, img Image) error {
	if fault := checkFormat(name, img.Format); fault != nil {
		return fault
	}

	format := wireFormat(img.Format)

	if len(img.Data) > MaxImageBytes {
		return &ValidationError{Message: fmt.Sprintf(
			"image '%s' is %d bytes, and an image is at most %d. Resize it to at most %d pixels a side "+
				"or save it as a smaller JPEG", printable(name), len(img.Data), MaxImageBytes, MaxImageSide)}
	}

	config, err := configOf(format, img.Data)
	if err != nil {
		return err
	}

	if !sideFits(config.Width) || !sideFits(config.Height) {
		return &ValidationError{Message: fmt.Sprintf(
			"image '%s' is %d by %d pixels, and each side must be %d to %d. Resize it first",
			printable(name), config.Width, config.Height, MinImageSide, MaxImageSide)}
	}

	return nil
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

func checkReferences(req Request) error {
	if len(req.Assets) == 0 {
		return nil
	}

	refs := slices.Sorted(maps.Keys(req.Assets))
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

	missing, err := UnreferencedAssets(req)
	if err != nil {
		return err
	}

	if len(missing) > 0 {
		return notReferenced(missing[0])
	}

	return nil
}

// UnreferencedAssets lists, sorted, the references no string value of the state or questions names.
// At each place in a text the longest reference starting there counts, so one inside a longer one does not.
func UnreferencedAssets(req Request) ([]string, error) {
	texts, err := stringValues(req)
	if err != nil {
		return nil, err
	}

	return unreferenced(texts, slices.Sorted(maps.Keys(req.Assets))), nil
}

func stringValues(req Request) ([]string, error) {
	// Through JSON, so a state of any Go type, a json.RawMessage included, is walked as a raw body is.
	encoded, err := jsonv2.Marshal(struct {
		State     any       `json:"state"`
		Questions Questions `json:"questions"`
	}{State: req.State, Questions: req.Questions}, bodyOptions)
	if err != nil {
		return nil, unencodable(err)
	}

	members, err := readMembers(encoded)
	if err != nil {
		return nil, unencodable(err)
	}

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

func sideFits(side int) bool {
	return side >= MinImageSide && side <= MaxImageSide
}

func checkBodySize(size int) error {
	if size > MaxBodyBytes {
		return &ValidationError{Message: fmt.Sprintf(
			"the request body is %d bytes, and the API takes at most %d. Send fewer or smaller images",
			size, MaxBodyBytes)}
	}

	return nil
}
