package onesie

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

func inlineParts(members []member, urls map[string]string) ([]member, error) {
	grow := 0
	for _, url := range urls {
		grow += len(url) + len(`{"type":"image_url","image_url":{"url":""}}`)
	}

	rewriter := &inliner{
		matcher: assetMatcher{refs: slices.Sorted(maps.Keys(urls))}, used: map[string]bool{}, render: partsRenderer(urls),
		grow: grow,
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

func assetsToImages(members []member, urls map[string]string) ([]member, error) {
	if slices.ContainsFunc(members, named("images")) {
		return nil, &ValidationError{Message: "a body for clef carries assets or images, not both"}
	}

	rewriter := &inliner{matcher: assetMatcher{refs: slices.Sorted(maps.Keys(urls))}, used: map[string]bool{}}
	rewriter.render = rewriter.labels

	rewritten, err := rewriter.rewrite(members)
	if err != nil {
		return nil, err
	}

	images, err := imageList(rewriter.order, urls)
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

		assets, err := readAssets(m.value)
		if err != nil {
			return nil, validationOr(err, "assets must be an object of images")
		}

		for _, ref := range slices.Sorted(maps.Keys(assets)) {
			url, err := dataURLOf(ref, assets[ref])
			if err != nil {
				return nil, err
			}

			urls[ref] = url
		}
	}

	return urls, nil
}

func validationOr(err error, message string) error {
	var validation *ValidationError
	if errors.As(err, &validation) {
		return err
	}

	return &ValidationError{Message: message}
}

func dataURLOf(ref string, asset rawAsset) (string, error) {
	if fault := asset.fault(ref); fault != nil {
		return "", fault
	}

	encoded, err := textOf(asset.data)
	if err != nil {
		return "", validationOr(err, fmt.Sprintf("asset '%s' needs data_base64 as a string", printable(ref)))
	}

	return "data:" + mimeOf(wireFormat(asset.format)) + ";base64," + encoded, nil
}

func dataURLsFrom(assets Assets) map[string]string {
	urls := make(map[string]string, len(assets))
	for ref, img := range assets {
		// Converted inside the join, so the base64 is copied once.
		urls[ref] = "data:" + mimeOf(img.format) + ";base64," + string(img.base64())
	}

	return urls
}

func mimeOf(format string) string {
	if format == "jpg" {
		return "image/jpeg"
	}

	return "image/png"
}

func imageList(order []string, urls map[string]string) (jsontext.Value, error) {
	size := len("[]")
	for _, ref := range order {
		size += len(urls[ref]) + len(`"",`)
	}

	out := encodeBuffer(size)

	enc := jsontext.NewEncoder(out, rawOptions()...)
	if err := enc.WriteToken(jsontext.BeginArray); err != nil {
		return nil, err
	}

	for _, ref := range order {
		if err := enc.WriteToken(jsontext.String(urls[ref])); err != nil {
			return nil, err
		}
	}

	if err := enc.WriteToken(jsontext.EndArray); err != nil {
		return nil, err
	}

	return withoutNewline(out.Bytes()), nil
}

func (r *inliner) member(m member) (jsontext.Value, error) {
	if m.name != "state" && m.name != "questions" {
		return m.value, nil
	}

	write := r.value
	if m.name == "questions" {
		write = r.questions
	}

	// Only a member that names an image gains its data URL. A miss here, as with an escaped reference,
	// only means the buffer grows as it is written.
	grow := 0
	if slices.ContainsFunc(r.matcher.refs, func(ref string) bool { return bytes.Contains(m.value, []byte(ref)) }) {
		grow = r.grow
	}

	out := encodeBuffer(len(m.value) + grow)

	enc := jsontext.NewEncoder(out, rawOptions()...)
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

		if holdsNoReferences(key) {
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

func partsRenderer(urls map[string]string) func(*jsontext.Encoder, []piece) error {
	write := func(enc *jsontext.Encoder, p piece) error {
		if p.ref != "" {
			return writePart(enc, urls[p.ref])
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
	// grow is about how much a rewritten member gains, so its buffer is sized once.
	grow int
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

func writePart(enc *jsontext.Encoder, url string) error {
	// Tokens rather than prebuilt bytes, so the encoder escapes a URL read from a raw body whatever it holds.
	for _, token := range []jsontext.Token{
		jsontext.BeginObject, jsontext.String("type"), jsontext.String("image_url"),
		jsontext.String("image_url"), jsontext.BeginObject, jsontext.String("url"), jsontext.String(url),
		jsontext.EndObject, jsontext.EndObject,
	} {
		if err := enc.WriteToken(token); err != nil {
			return err
		}
	}

	return nil
}
