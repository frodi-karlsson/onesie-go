package onesie

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"
)

// Result is one response from the evaluation endpoint.
type Result struct {
	Model     string            `json:"model"`
	Answers   map[string]Answer `json:"answers"`
	Usage     Usage             `json:"usage"`
	RequestID string            `json:"request_id,omitempty"`

	// Header is the full response header, which carries rate limit information the typed fields
	// do not. It is not part of the wire shape.
	Header http.Header `json:"-"`
}

// Noul returns the named answer, or an AnswerError when it is absent or of another type.
func (r *Result) Noul(name string) (*NoulAnswer, error) {
	found, err := r.answer(name)
	if err != nil {
		return nil, err
	}

	typed, ok := found.(*NoulAnswer)
	if !ok {
		return nil, &AnswerError{Name: name, Want: "noul", Got: found.Kind()}
	}

	return typed, nil
}

// Choice returns the named answer, or an AnswerError when it is absent or of another type.
func (r *Result) Choice(name string) (*ChoiceAnswer, error) {
	found, err := r.answer(name)
	if err != nil {
		return nil, err
	}

	typed, ok := found.(*ChoiceAnswer)
	if !ok {
		return nil, &AnswerError{Name: name, Want: "choice", Got: found.Kind()}
	}

	return typed, nil
}

// Score returns the named answer, or an AnswerError when it is absent or of another type.
func (r *Result) Score(name string) (*ScoreAnswer, error) {
	found, err := r.answer(name)
	if err != nil {
		return nil, err
	}

	typed, ok := found.(*ScoreAnswer)
	if !ok {
		return nil, &AnswerError{Name: name, Want: "score", Got: found.Kind()}
	}

	return typed, nil
}

// Box returns the named answer, or an AnswerError when it is absent or of another type.
func (r *Result) Box(name string) (*BoxAnswer, error) {
	found, err := r.answer(name)
	if err != nil {
		return nil, err
	}

	typed, ok := found.(*BoxAnswer)
	if !ok {
		return nil, &AnswerError{Name: name, Want: "box", Got: found.Kind()}
	}

	return typed, nil
}

// UnmarshalJSON decodes each answer into its concrete type, dispatching on the wire type field.
func (r *Result) UnmarshalJSON(data []byte) error {
	var wire struct {
		Model   string                     `json:"model"`
		Answers map[string]json.RawMessage `json:"answers"`
		Usage   Usage                      `json:"usage"`
	}

	if err := json.Unmarshal(data, &wire); err != nil {
		return describeShape(err)
	}

	r.Model = wire.Model
	r.Usage = wire.Usage
	r.Answers = make(map[string]Answer, len(wire.Answers))

	for _, name := range slices.Sorted(maps.Keys(wire.Answers)) {
		decoded, err := decodeAnswer(wire.Answers[name])
		if err != nil {
			return namingAnswer(name, err)
		}

		r.Answers[name] = decoded
	}

	return nil
}

func describeShape(err error) error {
	var mistyped *json.UnmarshalTypeError
	if !errors.As(err, &mistyped) {
		return err
	}

	return &resultShapeError{
		field: strings.TrimPrefix(mistyped.Field, "."),
		fault: faultWrongKind,
		want:  wantedKind(mistyped.Type),
		got:   foundKind(mistyped.Value),
		cause: err,
	}
}

func wantedKind(wanted reflect.Type) jsonKind {
	for wanted != nil && wanted.Kind() == reflect.Pointer {
		wanted = wanted.Elem()
	}

	if wanted == nil {
		return kindNothing
	}

	switch wanted.Kind() {
	case reflect.Float32, reflect.Float64, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return kindNumber
	case reflect.String:
		return kindString
	case reflect.Bool:
		return kindBoolean
	case reflect.Slice, reflect.Array:
		return kindArray
	default:
		return kindObject
	}
}

func foundKind(value string) jsonKind {
	kind, _, _ := strings.Cut(value, " ")

	switch kind {
	case "string":
		return kindString
	case "number":
		return kindNumber
	case "bool":
		return kindBoolean
	case "array":
		return kindArray
	case "object":
		return kindObject
	default:
		return kindNothing
	}
}

func namingAnswer(name string, err error) error {
	var shape *resultShapeError
	if !errors.As(err, &shape) {
		return fmt.Errorf("answer '%s': %w", printable(name), err)
	}

	shape.answer = name

	return shape
}

func (r *Result) answer(name string) (Answer, error) {
	found, ok := r.Answers[name]
	if !ok {
		return nil, &AnswerError{Name: name, Missing: true}
	}

	return found, nil
}

// Usage is the token count the API billed for a request, and the cost in USD when the provider reports one.
type Usage struct {
	InputTokens  int      `json:"input_tokens"`
	OutputTokens int      `json:"output_tokens"`
	Cost         *float64 `json:"cost,omitempty"`
}

func decodeAnswer(raw json.RawMessage) (Answer, error) {
	if kind := rawKind(raw); kind != kindObject {
		return nil, &resultShapeError{want: kindObject, got: kind}
	}

	var probe struct {
		Type *string `json:"type"`
	}

	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, describeShape(err)
	}

	if probe.Type == nil {
		return nil, missingField("type", kindString)
	}

	switch *probe.Type {
	case "noul":
		return decodeNoul(raw)
	case "choice":
		return decodeChoice(raw)
	case "score":
		return decodeScore(raw)
	case "box":
		return decodeBox(raw)
	case "":
		return nil, &resultShapeError{field: "type", fault: faultEmpty, want: kindString}
	default:
		return nil, &resultShapeError{field: "type", fault: faultUnknownType, gotType: *probe.Type}
	}
}

func rawKind(raw json.RawMessage) jsonKind {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return kindNothing
	}

	switch trimmed[0] {
	case '{':
		return kindObject
	case '[':
		return kindArray
	case '"':
		return kindString
	case 'n':
		return kindNull
	case 't', 'f':
		return kindBoolean
	default:
		return kindNumber
	}
}

func decodeNoul(raw json.RawMessage) (*NoulAnswer, error) {
	var wire struct {
		Noul *float64 `json:"noul"`
	}

	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, describeShape(err)
	}

	if wire.Noul == nil {
		return nil, missingField("noul", kindNumber)
	}

	return &NoulAnswer{Noul: *wire.Noul}, nil
}

func decodeChoice(raw json.RawMessage) (*ChoiceAnswer, error) {
	var wire struct {
		Choice        *string             `json:"choice"`
		Confidence    float64             `json:"confidence"`
		Probabilities map[string]*float64 `json:"probabilities"`
	}

	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, describeShape(err)
	}

	if wire.Choice == nil {
		return nil, missingField("choice", kindString)
	}

	if *wire.Choice == "" {
		return nil, &resultShapeError{field: "choice", fault: faultEmpty, want: kindString}
	}

	probabilities, err := requireProbabilities(wire.Probabilities)
	if err != nil {
		return nil, err
	}

	return &ChoiceAnswer{Choice: *wire.Choice, Confidence: wire.Confidence, Probabilities: probabilities}, nil
}

func decodeScore(raw json.RawMessage) (*ScoreAnswer, error) {
	var wire struct {
		Score         *float64            `json:"score"`
		Confidence    float64             `json:"confidence"`
		Legend        map[string]string   `json:"legend"`
		Probabilities map[string]*float64 `json:"probabilities"`
	}

	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, describeShape(err)
	}

	if wire.Score == nil {
		return nil, missingField("score", kindNumber)
	}

	probabilities, err := requireProbabilities(wire.Probabilities)
	if err != nil {
		return nil, err
	}

	return &ScoreAnswer{
		Score: *wire.Score, Confidence: wire.Confidence, Legend: wire.Legend, Probabilities: probabilities,
	}, nil
}

func decodeBox(raw json.RawMessage) (*BoxAnswer, error) {
	var wire struct {
		Box *struct {
			Top    *float64 `json:"top"`
			Bottom *float64 `json:"bottom"`
			Left   *float64 `json:"left"`
			Right  *float64 `json:"right"`
		} `json:"box"`
		Confidence float64 `json:"confidence"`
	}

	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, describeShape(err)
	}

	if wire.Box == nil {
		return nil, missingField("box", kindObject)
	}

	edges := []struct {
		name  string
		value *float64
	}{
		{"top", wire.Box.Top}, {"bottom", wire.Box.Bottom}, {"left", wire.Box.Left}, {"right", wire.Box.Right},
	}

	for _, edge := range edges {
		if edge.value == nil {
			return nil, missingField("box."+edge.name, kindNumber)
		}
	}

	return &BoxAnswer{
		Edges:      Edges{Top: *wire.Box.Top, Bottom: *wire.Box.Bottom, Left: *wire.Box.Left, Right: *wire.Box.Right},
		Confidence: wire.Confidence,
	}, nil
}

func missingField(field string, want jsonKind) error {
	return &resultShapeError{field: field, fault: faultMissing, want: want}
}

func requireProbabilities(wire map[string]*float64) (map[string]float64, error) {
	if wire == nil {
		return nil, missingField("probabilities", kindObject)
	}

	if len(wire) == 0 {
		return nil, &resultShapeError{field: "probabilities", fault: faultEmpty, want: kindObject}
	}

	probabilities := make(map[string]float64, len(wire))

	for _, option := range slices.Sorted(maps.Keys(wire)) {
		if wire[option] == nil {
			return nil, missingField("probabilities."+option, kindNumber)
		}

		probabilities[option] = *wire[option]
	}

	return probabilities, nil
}

// Answer is one of NoulAnswer, ChoiceAnswer, ScoreAnswer or BoxAnswer.
type Answer interface {
	json.Marshaler

	// Kind reports the wire type, one of noul, choice, score or box.
	Kind() string
}

// NoulAnswer carries no confidence. Its single value describes a two outcome distribution fully.
type NoulAnswer struct {
	Noul float64 `json:"noul"`
}

// Kind reports the wire type.
func (*NoulAnswer) Kind() string {
	return "noul"
}

// MarshalJSON re-emits the wire shape, including the type discriminator.
func (a *NoulAnswer) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type string  `json:"type"`
		Noul float64 `json:"noul"`
	}{Type: "noul", Noul: a.Noul})
}

// ChoiceAnswer reports the winning option and the distribution over all of them.
type ChoiceAnswer struct {
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// Kind reports the wire type.
func (*ChoiceAnswer) Kind() string {
	return "choice"
}

// Probability reports one option's probability. The second result is false when the option was not
// part of the answer, which the compiler cannot rule out.
func (a *ChoiceAnswer) Probability(option string) (float64, bool) {
	value, ok := a.Probabilities[option]

	return value, ok
}

// MarshalJSON re-emits the wire shape, including the type discriminator.
func (a *ChoiceAnswer) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type          string             `json:"type"`
		Choice        string             `json:"choice"`
		Confidence    float64            `json:"confidence"`
		Probabilities map[string]float64 `json:"probabilities"`
	}{
		Type: "choice", Choice: a.Choice,
		Confidence: a.Confidence, Probabilities: a.Probabilities,
	})
}

// ScoreAnswer reports a probability weighted value, so it can land between levels. Legend and
// Probabilities are keyed by the level index as a string, matching the wire format.
type ScoreAnswer struct {
	Score         float64            `json:"score"`
	Confidence    float64            `json:"confidence"`
	Legend        map[string]string  `json:"legend"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// Kind reports the wire type.
func (*ScoreAnswer) Kind() string {
	return "score"
}

// MarshalJSON re-emits the wire shape, including the type discriminator.
func (a *ScoreAnswer) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type          string             `json:"type"`
		Score         float64            `json:"score"`
		Confidence    float64            `json:"confidence"`
		Legend        map[string]string  `json:"legend"`
		Probabilities map[string]float64 `json:"probabilities"`
	}{
		Type: "score", Score: a.Score, Confidence: a.Confidence,
		Legend: a.Legend, Probabilities: a.Probabilities,
	})
}

// BoxAnswer is a Box's edges, each a fraction of the image from its top left corner. The edges are
// not range checked here, so one bad box cannot fail every other answer in its result.
type BoxAnswer struct {
	Edges      Edges   `json:"box"`
	Confidence float64 `json:"confidence"`
}

// Kind reports the wire type.
func (*BoxAnswer) Kind() string {
	return "box"
}

// MarshalJSON re-emits the wire shape, including the type discriminator.
func (a *BoxAnswer) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type       string  `json:"type"`
		Edges      Edges   `json:"box"`
		Confidence float64 `json:"confidence"`
	}{Type: "box", Edges: a.Edges, Confidence: a.Confidence})
}

// Edges are the sides of a box, as fractions of the image's width and height.
type Edges struct {
	Top    float64 `json:"top"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
	Right  float64 `json:"right"`
}
