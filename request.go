package onesie

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

const (
	maxReferenceBytes     = 1024
	maxAllReferencesBytes = 65536
)

// ValidateRequest rejects a request the API would reject, before it is sent. It checks the
// questions, then the assets, then the size of the body.
func ValidateRequest(req Request) error {
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

	return checkBodySize(req)
}

// ValidateImage rejects an image the API would reject, naming it in the message. It checks the
// format, the file size and the sides, reading only the file's header.
func ValidateImage(name string, img Image) error {
	if fault := checkFormat(name, img.Format); fault != nil {
		return fault
	}

	format := wireFormat(img.Format)

	if len(img.Data) > MaxImageBytes {
		return &ValidationError{Message: fmt.Sprintf(
			"image '%s' is %d bytes, and an image is at most %d. Resize it to at most %d pixels a side "+
				"or save it as a smaller JPEG", name, len(img.Data), MaxImageBytes, MaxImageSide)}
	}

	config, err := configOf(format, img.Data)
	if err != nil {
		return err
	}

	if !sideFits(config.Width) || !sideFits(config.Height) {
		return &ValidationError{Message: fmt.Sprintf(
			"image '%s' is %d by %d pixels, and each side must be %d to %d. Resize it first",
			name, config.Width, config.Height, MinImageSide, MaxImageSide)}
	}

	return nil
}

func checkBoxes(req Request) error {
	if len(req.Assets) > 0 {
		return nil
	}

	for _, named := range req.Questions {
		if _, isBox := named.Question.(Box); isBox {
			return &ValidationError{
				Question: named.ID,
				Message:  fmt.Sprintf("box question '%s' needs an image in the request", named.ID),
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
				ref, len(ref), maxReferenceBytes)}
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
	// Through JSON, so a state of any Go type, a json.RawMessage included, is walked the same way.
	// Only values are collected, since the API never replaces a reference in an object key.
	encoded, err := json.Marshal(struct {
		State     any       `json:"state"`
		Questions Questions `json:"questions"`
	}{State: req.State, Questions: req.Questions})
	if err != nil {
		return nil, unencodable(err)
	}

	var decoded struct {
		State     any                       `json:"state"`
		Questions map[string]map[string]any `json:"questions"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, unencodable(err)
	}

	var texts []string

	collectStrings(decoded.State, &texts)

	for _, question := range decoded.Questions {
		// The type is onesie's own wire field, not text the API replaces references in.
		delete(question, "type")
		collectStrings(question, &texts)
	}

	return texts, nil
}

func unencodable(err error) error {
	return &ValidationError{Message: "the request cannot be encoded: " + err.Error()}
}

func collectStrings(value any, texts *[]string) {
	switch typed := value.(type) {
	case string:
		*texts = append(*texts, typed)
	case []any:
		for _, item := range typed {
			collectStrings(item, texts)
		}
	case map[string]any:
		for _, item := range typed {
			collectStrings(item, texts)
		}
	}
}

func sideFits(side int) bool {
	return side >= MinImageSide && side <= MaxImageSide
}

func checkBodySize(req Request) error {
	if len(req.Assets) == 0 {
		return nil
	}

	body, err := MarshalBody(req)
	if err != nil {
		return unencodable(err)
	}

	if len(body) > MaxBodyBytes {
		return &ValidationError{Message: fmt.Sprintf(
			"the request body is %d bytes, and the API takes at most %d. Send fewer or smaller images",
			len(body), MaxBodyBytes)}
	}

	return nil
}
