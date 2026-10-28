package onesie

import (
	"errors"
	"testing"
)

func TestAnswerAs(t *testing.T) {
	t.Parallel()

	result := &Result{Answers: map[string]Answer{"n": &NoulAnswer{Noul: 0.5}}}

	t.Run("should return the answer as its own type", func(t *testing.T) {
		t.Parallel()

		got, err := answerAs[*NoulAnswer](result, "n")
		if err != nil || got.Noul != 0.5 {
			t.Fatalf("answerAs = %+v, %v, want the noul at 0.5", got, err)
		}
	})

	t.Run("should name the kind asked for and the kind found", func(t *testing.T) {
		t.Parallel()

		got, err := answerAs[*BoxAnswer](result, "n")

		var wrong *AnswerError
		if !errors.As(err, &wrong) || got != nil || *wrong != (AnswerError{Name: "n", Want: "box", Got: "noul"}) {
			t.Fatalf("answerAs = %+v, %v, want a box asked for and a noul found", got, err)
		}
	})

	t.Run("should report an answer that is absent", func(t *testing.T) {
		t.Parallel()

		got, err := answerAs[*ChoiceAnswer](result, "x")

		var missing *AnswerError
		if !errors.As(err, &missing) || got != nil || !missing.Missing || missing.Name != "x" {
			t.Fatalf("answerAs = %+v, %v, want x missing", got, err)
		}
	})
}
