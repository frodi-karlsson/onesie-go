package onesie

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

const answerKinds = "noul, choice, score or box"

const (
	faultWrongKind shapeFault = iota
	faultMissing
	faultEmpty
	faultUnknownType
)

const (
	kindNothing jsonKind = iota
	kindObject
	kindArray
	kindString
	kindNumber
	kindFiniteNumber
	kindWholeNumber
	kindBoolean
	kindNull
)

// Check returns an *AnswerFault for the first question, in question order, whose answer is missing
// or does not fit it, or nil. SystemOne and SendPrepared check every result they return.
func (r *Result) Check(questions Questions) error {
	for _, named := range questions {
		if fault := checkAnswer(named.ID, dereferenced(named.Question), r.Answers[named.ID]); fault != nil {
			return fault
		}
	}

	return nil
}

func checkAnswer(id string, asked Question, answer Answer) *AnswerFault {
	if answer == nil {
		return misfit(id, "question '%s' has no answer")
	}

	if want := kindOf(asked); want != "" && want != answer.Kind() {
		return misfit(id, "question '%s' expects a %s answer, got %s", want, answer.Kind())
	}

	switch typed := answer.(type) {
	case *NoulAnswer:
		if !unit(typed.Noul) {
			return misfit(id, "question '%s' answered %s, which lies outside [0,1]", formatNumber(typed.Noul))
		}
	case *ChoiceAnswer:
		if choice, isChoice := asked.(Choice); isChoice {
			return checkChoice(id, optionNames(choice.Criteria), typed)
		}
	case *ScoreAnswer:
		if score, isScore := asked.(Score); isScore {
			return checkScore(id, len(score.Criteria), typed)
		}
	case *BoxAnswer:
		return checkBox(id, typed)
	}

	return nil
}

func dereferenced(asked Question) Question {
	switch typed := asked.(type) {
	case *Noul:
		if typed != nil {
			return *typed
		}
	case *Choice:
		if typed != nil {
			return *typed
		}
	case *Score:
		if typed != nil {
			return *typed
		}
	case *Box:
		if typed != nil {
			return *typed
		}
	}

	return asked
}

func kindOf(asked Question) string {
	switch asked.(type) {
	case Noul:
		return "noul"
	case Choice:
		return "choice"
	case Score:
		return "score"
	case Box:
		return "box"
	default:
		return ""
	}
}

func optionNames(criteria Criteria) []string {
	names := make([]string, 0, len(criteria))

	for _, named := range criteria {
		if !slices.Contains(names, named.Name) {
			names = append(names, named.Name)
		}
	}

	return names
}

func checkChoice(id string, names []string, answer *ChoiceAnswer) *AnswerFault {
	options := printableList(names)

	if !slices.Contains(names, answer.Choice) {
		return misfit(id, "question '%s' picked '%s', which is not one of its options: %s", printable(answer.Choice), options)
	}

	for _, key := range slices.Sorted(maps.Keys(answer.Probabilities)) {
		if !slices.Contains(names, key) {
			return misfit(id, "question '%s' has p for '%s', which is not one of its options: %s", printable(key), options)
		}
	}

	for _, name := range names {
		if _, found := answer.Probabilities[name]; !found {
			return misfit(id, "question '%s' has no p for '%s'", printable(name))
		}
	}

	return checkSpread(id, names, answer.Probabilities, answer.Confidence)
}

func printableList(names []string) string {
	shown := make([]string, 0, len(names))
	for _, name := range names {
		shown = append(shown, printable(name))
	}

	return strings.Join(shown, ", ")
}

func checkScore(id string, levels int, answer *ScoreAnswer) *AnswerFault {
	if levels == 0 {
		return misfit(id, "question '%s' has no levels to score against")
	}

	top := levels - 1

	if math.IsNaN(answer.Score) || answer.Score < 0 || answer.Score > float64(top) {
		return misfit(id, "question '%s' has score %s, which lies outside [0,%d]", formatNumber(answer.Score), top)
	}

	indexes := make([]string, 0, levels)
	for i := range levels {
		indexes = append(indexes, strconv.Itoa(i))
	}

	for _, key := range NumericOrder(slices.Collect(maps.Keys(answer.Probabilities))) {
		if !slices.Contains(indexes, key) {
			return misfit(id, "question '%s' has p for '%s', which is not a level index from 0 to %d", printable(key), top)
		}
	}

	for i, index := range indexes {
		if _, found := answer.Probabilities[index]; !found {
			return misfit(id, "question '%s' has no p for level %d", i)
		}
	}

	return checkSpread(id, indexes, answer.Probabilities, answer.Confidence)
}

// NumericOrder returns a sorted copy of keys with level indexes first by number, then every other key
// by text. A key counts as an index only when written as strconv.Itoa writes it, so 01 and +1 are text.
func NumericOrder(keys []string) []string {
	return slices.SortedFunc(slices.Values(keys), func(left, right string) int {
		leftIndex, leftIsIndex := levelIndex(left)
		rightIndex, rightIsIndex := levelIndex(right)

		switch {
		case leftIsIndex && rightIsIndex:
			return cmp.Compare(leftIndex, rightIndex)
		case leftIsIndex:
			return -1
		case rightIsIndex:
			return 1
		default:
			return strings.Compare(left, right)
		}
	})
}

func levelIndex(key string) (int, bool) {
	index, err := strconv.Atoi(key)

	return index, err == nil && strconv.Itoa(index) == key
}

func checkSpread(id string, keys []string, probabilities map[string]float64, confidence float64) *AnswerFault {
	sum := 0.0

	for _, key := range keys {
		value := probabilities[key]
		if !unit(value) {
			return misfit(id, "question '%s' has p for '%s' of %s, which lies outside [0,1]", printable(key), formatNumber(value))
		}

		sum += value
	}

	// A provider that writes two places per entry leaves the sum a hundredth or two off 1.
	if math.Abs(sum-1) > 0.02+1e-9 {
		return misfit(id, "question '%s' has p that adds up to %s, not 1", formatNumber(math.Round(sum*1e4)/1e4))
	}

	if !unit(confidence) {
		return misfit(id, "question '%s' has confidence %s, which lies outside [0,1]", formatNumber(confidence))
	}

	return nil
}

func checkBox(id string, answer *BoxAnswer) *AnswerFault {
	edges := answer.Edges

	if !unit(edges.Left) || !unit(edges.Top) || !unit(edges.Right) || !unit(edges.Bottom) || !unit(answer.Confidence) {
		return misfit(id, "box question '%s' has an edge or a confidence outside [0,1]")
	}

	if edges.Left >= edges.Right || edges.Top >= edges.Bottom {
		return misfit(id, "box question '%s' has a left edge not left of its right edge, or a top edge not above its bottom edge")
	}

	return nil
}

func misfit(id, format string, args ...any) *AnswerFault {
	return &AnswerFault{Question: id, text: fmt.Sprintf(format, append([]any{printable(id)}, args...)...)}
}

func unit(value float64) bool {
	return !math.IsNaN(value) && value >= 0 && value <= 1
}

func formatNumber(value float64) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return strconv.FormatFloat(value, 'g', -1, 64)
	}

	return string(encoded)
}

type shapeFault int

type jsonKind int

func resultSentence(field string, fault shapeFault, want, got jsonKind) string {
	const lead = "the response is not a System One result"

	switch {
	case field == "":
		return lead + ", since it is " + got.phrase()
	case fault == faultMissing:
		return lead + ", since it has no " + field + ", or it is null"
	default:
		_, _, verb := fieldPhrase(field)

		return lead + ", since its " + field + " " + verb + " not " + want.phrase()
	}
}

func (k jsonKind) phrase() string {
	switch k {
	case kindObject:
		return "an object"
	case kindArray:
		return "an array"
	case kindString:
		return "a string"
	case kindNumber:
		return "a number"
	case kindFiniteNumber:
		return "a finite number"
	case kindWholeNumber:
		return "a whole number"
	case kindBoolean:
		return "a boolean"
	case kindNull:
		return "null"
	default:
		return "nothing"
	}
}

func fieldPhrase(field string) (phrase, bare, verb string) {
	parent, key, nested := strings.Cut(field, ".")

	switch {
	case nested && parent == "probabilities":
		bare = "probability for '" + key + "'"
	case nested && parent == "legend":
		bare = "legend entry for '" + key + "'"
	case nested && parent == "box":
		bare = "box " + key + " edge"
	case !nested && strings.HasSuffix(field, "s"):
		return field, field, "are"
	default:
		bare = field
	}

	if strings.ContainsAny(bare[:1], "aeiou") {
		return "an " + bare, bare, "is"
	}

	return "a " + bare, bare, "is"
}
