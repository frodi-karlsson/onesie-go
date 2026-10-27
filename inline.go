package onesie

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

func inlineParts(members []member) ([]member, error) {
	urls, err := dataURLsOf(members)
	if err != nil {
		return nil, err
	}

	parts, err := partsFrom(urls)
	if err != nil {
		return nil, err
	}

	rewriter := &inliner{
		matcher: assetMatcher{refs: slices.Sorted(maps.Keys(urls))}, used: map[string]bool{}, render: partsRenderer(parts),
	}

	rewritten, err := rewriter.rewrite(members)
	if err != nil {
		return nil, err
	}

	size, err := encodedSize(rewritten)
	if err != nil {
		return nil, err
	}

	if size > MaxBodyBytes {
		return nil, &ValidationError{Message: fmt.Sprintf(
			"the request body is %d bytes once each image is placed where it is named, and the API takes "+
				"at most %d. This provider takes an image once per place it is named, so name each image once",
			size, MaxBodyBytes)}
	}

	return rewritten, nil
}

func assetsToImages(members []member) ([]member, error) {
	if slices.ContainsFunc(members, named("images")) {
		return nil, &ValidationError{Message: "a body for clef carries assets or images, not both"}
	}

	urls, err := dataURLsOf(members)
	if err != nil {
		return nil, err
	}

	rewriter := &inliner{matcher: assetMatcher{refs: slices.Sorted(maps.Keys(urls))}, used: map[string]bool{}}
	rewriter.render = rewriter.labels

	rewritten, err := rewriter.rewrite(members)
	if err != nil {
		return nil, err
	}

	ordered := make([]string, 0, len(rewriter.order))
	for _, ref := range rewriter.order {
		ordered = append(ordered, urls[ref])
	}

	images, err := json.Marshal(ordered)
	if err != nil {
		return nil, err
	}

	return append(rewritten, member{name: "images", value: images}), nil
}

func (r *inliner) rewrite(members []member) ([]member, error) {
	rewritten := make([]member, 0, len(members))

	for _, m := range members {
		if m.name == "assets" {
			continue
		}

		value, err := r.member(m)
		if err != nil {
			return nil, err
		}

		rewritten = append(rewritten, member{name: m.name, value: value})
	}

	return rewritten, nil
}

func dataURLsOf(members []member) (map[string]string, error) {
	urls := map[string]string{}

	for _, m := range members {
		if m.name != "assets" {
			continue
		}

		var assets map[string]struct {
			Format any `json:"format"`
			Data   any `json:"data_base64"`
		}
		if err := json.Unmarshal(m.value, &assets); err != nil {
			return nil, &ValidationError{Message: "assets must be an object of images"}
		}

		for ref, asset := range assets {
			url, err := dataURLOf(ref, asset.Format, asset.Data)
			if err != nil {
				return nil, err
			}

			urls[ref] = url
		}
	}

	return urls, nil
}

func dataURLOf(ref string, format, data any) (string, error) {
	formatText := textOf(format)
	if fault := checkFormat(ref, formatText); fault != nil {
		return "", fault
	}

	encoded, isText := data.(string)
	if !isText {
		return "", &ValidationError{Message: fmt.Sprintf("asset '%s' needs data_base64 as a string", ref)}
	}

	mime := "image/png"
	if wireFormat(formatText) == "jpg" {
		mime = "image/jpeg"
	}

	return "data:" + mime + ";base64," + encoded, nil
}

func textOf(value any) string {
	if text, isText := value.(string); isText {
		return text
	}

	return ""
}

func partsFrom(urls map[string]string) (map[string]jsontext.Value, error) {
	parts := make(map[string]jsontext.Value, len(urls))

	for ref, url := range urls {
		part, err := json.Marshal(imagePart{Type: "image_url", ImageURL: imageURL{URL: url}})
		if err != nil {
			return nil, err
		}

		parts[ref] = part
	}

	return parts, nil
}

type imagePart struct {
	Type     string   `json:"type"`
	ImageURL imageURL `json:"image_url"`
}

type imageURL struct {
	URL string `json:"url"`
}

func (r *inliner) member(m member) (jsontext.Value, error) {
	if m.name != "state" && m.name != "questions" {
		return m.value, nil
	}

	write := r.value
	if m.name == "questions" {
		write = r.questions
	}

	var out bytes.Buffer

	enc := jsontext.NewEncoder(&out, rawOptions()...)
	if err := write(enc, jsontext.NewDecoder(bytes.NewBuffer(m.value), rawOptions()...)); err != nil {
		return nil, err
	}

	// The encoder ends a top level value with a newline, which a member does not need.
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

func (r *inliner) questions(enc *jsontext.Encoder, dec *jsontext.Decoder) error {
	if dec.PeekKind() != '{' {
		return r.value(enc, dec)
	}

	if err := copyToken(enc, dec); err != nil {
		return err
	}

	for dec.PeekKind() != '}' {
		if err := copyValue(enc, dec); err != nil {
			return err
		}

		if err := r.question(enc, dec); err != nil {
			return err
		}
	}

	return copyToken(enc, dec)
}

func (r *inliner) question(enc *jsontext.Encoder, dec *jsontext.Decoder) error {
	if dec.PeekKind() != '{' {
		return r.value(enc, dec)
	}

	if err := copyToken(enc, dec); err != nil {
		return err
	}

	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return err
		}

		key := name.String()
		if err := enc.WriteToken(jsontext.String(key)); err != nil {
			return err
		}

		// The type is onesie's own wire field, not text the API replaces references in.
		if key == "type" {
			if err := copyValue(enc, dec); err != nil {
				return err
			}

			continue
		}

		if err := r.value(enc, dec); err != nil {
			return err
		}
	}

	return copyToken(enc, dec)
}

func (r *inliner) value(enc *jsontext.Encoder, dec *jsontext.Decoder) error {
	switch dec.PeekKind() {
	case '{':
		return r.object(enc, dec)
	case '[':
		return r.array(enc, dec)
	case '"':
		raw, err := dec.ReadValue()
		if err != nil {
			return err
		}

		return r.text(enc, raw)
	default:
		return copyValue(enc, dec)
	}
}

func (r *inliner) object(enc *jsontext.Encoder, dec *jsontext.Decoder) error {
	if err := copyToken(enc, dec); err != nil {
		return err
	}

	for dec.PeekKind() != '}' {
		if err := copyValue(enc, dec); err != nil {
			return err
		}

		if err := r.value(enc, dec); err != nil {
			return err
		}
	}

	return copyToken(enc, dec)
}

func (r *inliner) array(enc *jsontext.Encoder, dec *jsontext.Decoder) error {
	if err := copyToken(enc, dec); err != nil {
		return err
	}

	for dec.PeekKind() != ']' {
		if err := r.value(enc, dec); err != nil {
			return err
		}
	}

	return copyToken(enc, dec)
}

func (r *inliner) text(enc *jsontext.Encoder, raw jsontext.Value) error {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return err
	}

	pieces := r.split(text)
	if pieces == nil {
		return enc.WriteValue(raw)
	}

	return r.render(enc, pieces)
}

func (r *inliner) split(text string) []piece {
	var pieces []piece

	start := 0

	for i := 0; i < len(text); {
		ref := r.matcher.longestAt(text[i:])
		if ref == "" {
			i++

			continue
		}

		if i > start {
			pieces = append(pieces, piece{text: text[start:i]})
		}

		pieces = append(pieces, piece{ref: ref})

		if !r.used[ref] {
			r.order = append(r.order, ref)
		}

		r.used[ref] = true
		i += len(ref)
		start = i
	}

	if pieces != nil && start < len(text) {
		pieces = append(pieces, piece{text: text[start:]})
	}

	return pieces
}

func partsRenderer(parts map[string]jsontext.Value) func(*jsontext.Encoder, []piece) error {
	write := func(enc *jsontext.Encoder, p piece) error {
		if p.ref != "" {
			return enc.WriteValue(parts[p.ref])
		}

		return enc.WriteToken(jsontext.String(p.text))
	}

	return func(enc *jsontext.Encoder, pieces []piece) error {
		if len(pieces) == 1 {
			return write(enc, pieces[0])
		}

		if err := enc.WriteToken(jsontext.BeginArray); err != nil {
			return err
		}

		for _, p := range pieces {
			if err := write(enc, p); err != nil {
				return err
			}
		}

		return enc.WriteToken(jsontext.EndArray)
	}
}

func (r *inliner) labels(enc *jsontext.Encoder, pieces []piece) error {
	var text strings.Builder

	for _, p := range pieces {
		if p.ref == "" {
			text.WriteString(p.text)

			continue
		}

		text.WriteString("image " + strconv.Itoa(slices.Index(r.order, p.ref)+1))
	}

	return enc.WriteToken(jsontext.String(text.String()))
}

type inliner struct {
	matcher assetMatcher
	used    map[string]bool
	order   []string
	render  func(*jsontext.Encoder, []piece) error
}

type piece struct {
	text string
	ref  string
}

func copyToken(enc *jsontext.Encoder, dec *jsontext.Decoder) error {
	token, err := dec.ReadToken()
	if err != nil {
		return err
	}

	return enc.WriteToken(token)
}

func copyValue(enc *jsontext.Encoder, dec *jsontext.Decoder) error {
	raw, err := dec.ReadValue()
	if err != nil {
		return err
	}

	return enc.WriteValue(raw)
}
