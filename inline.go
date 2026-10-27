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

var rawOptions = []jsontext.Options{jsontext.AllowDuplicateNames(true), jsontext.PreserveRawStrings(true)}

func inlineParts(body []byte) ([]byte, error) {
	members, err := readMembers(body)
	if err != nil {
		return nil, err
	}

	urls, err := dataURLsOf(members)
	if err != nil {
		return nil, err
	}

	parts, err := partsFrom(urls)
	if err != nil {
		return nil, err
	}

	rewriter := &inliner{refs: slices.Sorted(maps.Keys(urls)), used: map[string]bool{}, render: partsRenderer(parts)}

	rewritten, err := rewriter.rewrite(members, nil)
	if err != nil {
		return nil, err
	}

	if len(rewritten) > MaxBodyBytes {
		return nil, &ValidationError{Message: fmt.Sprintf(
			"the request body is %d bytes once each image is placed where it is named, and the API takes "+
				"at most %d. This provider takes an image once per place it is named, so name each image once",
			len(rewritten), MaxBodyBytes)}
	}

	return rewritten, nil
}

func assetsToImages(body []byte) ([]byte, error) {
	members, err := readMembers(body)
	if err != nil {
		return nil, err
	}

	if slices.ContainsFunc(members, func(m member) bool { return m.name == "images" }) {
		return nil, &ValidationError{Message: "a body for clef carries assets or images, not both"}
	}

	urls, err := dataURLsOf(members)
	if err != nil {
		return nil, err
	}

	rewriter := &inliner{refs: slices.Sorted(maps.Keys(urls)), used: map[string]bool{}}
	rewriter.render = rewriter.labels

	rewritten, err := rewriter.rewrite(members, func(enc *jsontext.Encoder) error {
		if writeErr := enc.WriteToken(jsontext.String("images")); writeErr != nil {
			return writeErr
		}

		ordered := make([]string, 0, len(rewriter.order))
		for _, ref := range rewriter.order {
			ordered = append(ordered, urls[ref])
		}

		images, marshalErr := json.Marshal(ordered)
		if marshalErr != nil {
			return marshalErr
		}

		return enc.WriteValue(images)
	})
	if err != nil {
		return nil, err
	}

	return rewritten, nil
}

func (r *inliner) rewrite(members []member, last func(*jsontext.Encoder) error) ([]byte, error) {
	var out bytes.Buffer

	enc := jsontext.NewEncoder(&out, rawOptions...)
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return nil, err
	}

	for _, m := range members {
		if m.name == "assets" {
			continue
		}

		if err := enc.WriteToken(jsontext.String(m.name)); err != nil {
			return nil, err
		}

		if err := r.member(enc, m); err != nil {
			return nil, err
		}
	}

	if err := r.checkUsed(); err != nil {
		return nil, err
	}

	if last != nil {
		if err := last(enc); err != nil {
			return nil, err
		}
	}

	if err := enc.WriteToken(jsontext.EndObject); err != nil {
		return nil, err
	}

	// The encoder ends a top level value with a newline, which a request body does not need.
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

func readMembers(body []byte) ([]member, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(body), rawOptions...)

	if _, err := dec.ReadToken(); err != nil {
		return nil, unreadableBody(err)
	}

	var members []member

	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return nil, unreadableBody(err)
		}

		key := name.String()

		value, err := dec.ReadValue()
		if err != nil {
			return nil, unreadableBody(err)
		}

		members = append(members, member{name: key, value: slices.Clone(value)})
	}

	return members, nil
}

func unreadableBody(err error) error {
	return &ValidationError{Message: "the request body cannot be read: " + err.Error()}
}

type member struct {
	name  string
	value jsontext.Value
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
	formatText, isText := format.(string)

	mime := map[string]string{"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg"}[strings.ToLower(formatText)]
	if !isText || mime == "" {
		return "", &ValidationError{Message: fmt.Sprintf("asset '%s' needs a format, png or jpg", ref)}
	}

	encoded, isText := data.(string)
	if !isText {
		return "", &ValidationError{Message: fmt.Sprintf("asset '%s' needs data_base64 as a string", ref)}
	}

	return "data:" + mime + ";base64," + encoded, nil
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

func (r *inliner) member(enc *jsontext.Encoder, m member) error {
	switch m.name {
	case "state":
		return r.value(enc, jsontext.NewDecoder(bytes.NewReader(m.value), rawOptions...))
	case "questions":
		return r.questions(enc, jsontext.NewDecoder(bytes.NewReader(m.value), rawOptions...))
	default:
		return enc.WriteValue(m.value)
	}
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
		ref := r.longestAt(text[i:])
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

func (r *inliner) longestAt(text string) string {
	longest := ""

	for _, ref := range r.refs {
		if len(ref) > len(longest) && strings.HasPrefix(text, ref) {
			longest = ref
		}
	}

	return longest
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

func (r *inliner) checkUsed() error {
	for _, ref := range r.refs {
		if !r.used[ref] {
			return &ValidationError{Message: fmt.Sprintf(
				"asset '%s' is not referenced in the state or the questions, and the API refuses that", ref)}
		}
	}

	return nil
}

type inliner struct {
	refs   []string
	used   map[string]bool
	order  []string
	render func(*jsontext.Encoder, []piece) error
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
