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
	return checkImage(name, img)
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

	unreferenced, err := UnreferencedAssets(req)
	if err != nil {
		return err
	}

	if len(unreferenced) > 0 {
		return &ValidationError{Message: fmt.Sprintf(
			"asset '%s' is not referenced in the state or the questions, and the API refuses that",
			unreferenced[0])}
	}

	return nil
}

// UnreferencedAssets lists, sorted, the references that appear in no string value of the request's
// state or questions. Object keys do not count, since the API never replaces a reference there.
func UnreferencedAssets(req Request) ([]string, error) {
	texts, err := stringValues(req)
	if err != nil {
		return nil, err
	}

	var unreferenced []string

	for _, ref := range slices.Sorted(maps.Keys(req.Assets)) {
		if !slices.ContainsFunc(texts, func(text string) bool { return strings.Contains(text, ref) }) {
			unreferenced = append(unreferenced, ref)
		}
	}

	return unreferenced, nil
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

func checkImage(ref string, img Image) error {
	format := wireFormat(img.Format)
	if format != "png" && format != "jpg" {
		return &ValidationError{Message: fmt.Sprintf(
			"image '%s' has format '%s', and an image must be png or jpg", ref, img.Format)}
	}

	if len(img.Data) > MaxImageBytes {
		return &ValidationError{Message: fmt.Sprintf(
			"image '%s' is %d bytes, and an image is at most %d. Resize it to at most %d pixels a side "+
				"or save it as a smaller JPEG", ref, len(img.Data), MaxImageBytes, MaxImageSide)}
	}

	config, err := configOf(format, img.Data)
	if err != nil {
		return err
	}

	if !sideFits(config.Width) || !sideFits(config.Height) {
		return &ValidationError{Message: fmt.Sprintf(
			"image '%s' is %d by %d pixels, and each side must be %d to %d. Resize it first",
			ref, config.Width, config.Height, MinImageSide, MaxImageSide)}
	}

	return nil
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
