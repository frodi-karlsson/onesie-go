package onesie_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/frodi-karlsson/onesie-go"
)

func TestQuestionMarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		question onesie.Question
		want     string
	}{
		{
			name:     "should marshal a bare noul without a criteria key",
			question: onesie.Noul{Instructions: "Does this convey urgency?"},
			want:     `{"type":"noul","instructions":"Does this convey urgency?"}`,
		},
		{
			name: "should marshal noul criteria",
			question: onesie.Noul{
				Instructions: "Urgent?",
				Criteria:     &onesie.NoulCriteria{True: "Time sensitive", False: "Not urgent"},
			},
			want: `{"type":"noul","instructions":"Urgent?","criteria":{"true":"Time sensitive","false":"Not urgent"}}`,
		},
		{
			name: "should marshal a choice with its criteria map",
			question: onesie.Choice{
				Instructions: "Which team?",
				Criteria: onesie.Criteria{
					{Name: "billing", Desc: "Payments"},
					{Name: "technical"},
				},
			},
			want: `{"type":"choice","instructions":"Which team?","criteria":{"billing":"Payments","technical":null}}`,
		},
		{
			name: "should marshal a score with ordered levels",
			question: onesie.Score{
				Instructions: "How severe?",
				Criteria:     onesie.Levels("Cosmetic", "Degraded", "Blocking"),
			},
			want: `{"type":"score","instructions":"How severe?","criteria":["Cosmetic","Degraded","Blocking"]}`,
		},
		{
			name:     "should marshal a box with only its instructions",
			question: onesie.Box{Instructions: "the coffee mug in `photo`"},
			want:     `{"type":"box","instructions":"the coffee mug in ` + "`photo`" + `"}`,
		},
		{
			name:     "should marshal structured instructions",
			question: onesie.Noul{Instructions: map[string]any{"question": "Same person?"}},
			want:     `{"type":"noul","instructions":{"question":"Same person?"}}`,
		},
		{
			name:     "should marshal nil instructions as null",
			question: onesie.Noul{},
			want:     `{"type":"noul","instructions":null}`,
		},
		{
			name:     "should marshal through a pointer",
			question: &onesie.Noul{Instructions: "Urgent?"},
			want:     `{"type":"noul","instructions":"Urgent?"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(tc.question)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if string(got) != tc.want {
				t.Errorf("\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

func TestQuestionsMarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		questions onesie.Questions
		want      string
		wantErr   string
	}{
		{
			name: "should marshal to an object in slice order",
			questions: onesie.Questions{
				{ID: "zebra", Question: onesie.Noul{Instructions: "z"}},
				{ID: "alpha", Question: onesie.Noul{Instructions: "a"}},
			},
			want: `{"zebra":{"type":"noul","instructions":"z"},` +
				`"alpha":{"type":"noul","instructions":"a"}}`,
		},
		{
			name:      "should marshal an empty set to an empty object",
			questions: onesie.Questions{},
			want:      `{}`,
		},
		{
			name:      "should marshal a nil set to an empty object",
			questions: nil,
			want:      `{}`,
		},
		{
			name: "should escape a key that carries a quote",
			questions: onesie.Questions{
				{ID: `a"b`, Question: onesie.Noul{Instructions: "q"}},
			},
			want: `{"a\"b":{"type":"noul","instructions":"q"}}`,
		},
		{
			name: "should fail when a question cannot be marshalled",
			questions: onesie.Questions{
				{ID: "a", Question: onesie.Noul{Instructions: "fine"}},
				{ID: "b", Question: onesie.Choice{
					Criteria: onesie.Criteria{{Name: "c", Desc: make(chan int)}},
				}},
			},
			wantErr: "unsupported type: chan int",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(tc.questions)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error, got %s", got)
				}

				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("\n got: %s\nwant it to contain: %s", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			if string(got) != tc.want {
				t.Errorf("Marshal = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestQuestionsUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want onesie.Questions
	}{
		{
			name: "should read each kind in order with its instructions and criteria",
			body: `{"u":{"type":"noul","instructions":"Urgent?","criteria":{"true":"now","false":{"z":1,"a":2}}},` +
				`"t":{"type":"choice","instructions":["a","b"],"criteria":{"billing":"Money","other":null}},` +
				`"r":{"type":"score","criteria":["low",{"k":"v"},3,true]},` +
				`"b":{"type":"box","instructions":"The cat"}}`,
			want: onesie.Questions{
				{ID: "u", Question: onesie.Noul{
					Instructions: "Urgent?",
					Criteria:     &onesie.NoulCriteria{True: "now", False: json.RawMessage(`{"z":1,"a":2}`)},
				}},
				{ID: "t", Question: onesie.Choice{
					Instructions: json.RawMessage(`["a","b"]`),
					Criteria:     onesie.Criteria{{Name: "billing", Desc: "Money"}, {Name: "other"}},
				}},
				{ID: "r", Question: onesie.Score{Criteria: []any{"low", json.RawMessage(`{"k":"v"}`), 3.0, true}}},
				{ID: "b", Question: onesie.Box{Instructions: "The cat"}},
			},
		},
		{
			name: "should keep the last of a repeated id at the last one's position",
			body: `{"a":{"type":"noul"},"b":{"type":"box"},"a":{"type":"box"}}`,
			want: onesie.Questions{{ID: "b", Question: onesie.Box{}}, {ID: "a", Question: onesie.Box{}}},
		},
		{
			name: "should keep the last of a repeated option at the last one's position",
			body: `{"t":{"type":"choice","criteria":{"x":"1","y":"2","x":"3"}}}`,
			want: onesie.Questions{{ID: "t", Question: onesie.Choice{Criteria: onesie.Criteria{{Name: "y", Desc: "2"}, {Name: "x", Desc: "3"}}}}},
		},
		{
			name: "should keep the last of a repeated field",
			body: `{"q":{"type":"box","type":"noul"}}`,
			want: onesie.Questions{{ID: "q", Question: onesie.Noul{}}},
		},
		{
			name: "should read a noul with null criteria as one with none",
			body: `{"q":{"type":"noul","criteria":null}}`,
			want: onesie.Questions{{ID: "q", Question: onesie.Noul{}}},
		},
		{name: "should read an empty object as no questions", body: `{}`, want: onesie.Questions{}},
		{name: "should read null as no questions", body: `null`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got onesie.Questions
			if err := json.Unmarshal([]byte(tc.body), &got); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Unmarshal = %#v, want %#v", got, tc.want)
			}
		})
	}

	t.Run("should leave questions it holds unchanged on null, as encoding/json does", func(t *testing.T) {
		t.Parallel()

		held := onesie.Questions{{ID: "q", Question: onesie.Box{}}}
		if err := json.Unmarshal([]byte(`null`), &held); err != nil || len(held) != 1 {
			t.Fatalf("Unmarshal = %#v, %v, want the held question kept", held, err)
		}
	})

	t.Run("should write an object it read back in its written key order", func(t *testing.T) {
		t.Parallel()

		const body = `{"q":{"type":"noul","instructions":{"z":1,"a":[2,1]}}}`

		var read onesie.Questions
		if err := json.Unmarshal([]byte(body), &read); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if written, err := json.Marshal(read); err != nil || string(written) != body {
			t.Errorf("Marshal = %s, %v, want %s", written, err, body)
		}
	})

	refusals := []struct {
		name     string
		body     string
		question string
		want     string
	}{
		{name: "should refuse questions that are not an object", body: `[1]`, want: "onesie: questions must be an object"},
		{name: "should refuse a question that is not an object", body: `{"q":"noul"}`, question: "q", want: "onesie: question 'q' is not an object"},
		{name: "should refuse a question with no type", body: `{"q":{"instructions":"x"}}`, question: "q", want: "onesie: question 'q' has no type"},
		{name: "should match the type name exactly", body: `{"q":{"Type":"noul"}}`, question: "q", want: "onesie: question 'q' has no type"},
		{name: "should refuse a type that is not a string", body: `{"q":{"type":1}}`, question: "q", want: "onesie: question 'q' has a type that is not a string"},
		{
			name: "should refuse an unknown type", body: `{"q":{"type":"tarot"}}`, question: "q",
			want: "onesie: question 'q' has the type 'tarot', which is not noul, choice, score or box",
		},
		{name: "should refuse a choice with no criteria", body: `{"t":{"type":"choice"}}`, question: "t", want: "onesie: choice question 't' needs criteria as an object"},
		{name: "should refuse a choice whose criteria are a list", body: `{"t":{"type":"choice","criteria":["a"]}}`, question: "t", want: "onesie: choice question 't' needs criteria as an object"},
		{name: "should refuse a score whose criteria are an object", body: `{"r":{"type":"score","criteria":{"a":1}}}`, question: "r", want: "onesie: score question 'r' needs criteria as an array"},
		{name: "should refuse a score with null criteria", body: `{"r":{"type":"score","criteria":null}}`, question: "r", want: "onesie: score question 'r' needs criteria as an array"},
		{name: "should refuse a noul whose criteria are text", body: `{"u":{"type":"noul","criteria":"yes"}}`, question: "u", want: "onesie: noul question 'u' needs criteria as an object, or none"},
		{
			name: "should refuse a number too large to read", body: `{"q":{"type":"noul","instructions":1e400}}`, question: "q",
			want: "onesie: question 'q' has a value that cannot be read: json: cannot unmarshal number 1e400 into Go value of type float64",
		},
	}

	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got onesie.Questions

			err := json.Unmarshal([]byte(tc.body), &got)

			var refused *onesie.ValidationError
			if !errors.As(err, &refused) || err.Error() != tc.want || refused.Question != tc.question {
				t.Fatalf("error = %v, want %q naming question %q", err, tc.want, tc.question)
			}
		})
	}
}

func TestValidateQuestions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		questions onesie.Questions
		wantErr   string
	}{
		{
			name:      "should reject an empty question set",
			questions: onesie.Questions{},
			wantErr:   "onesie: at least one question is required",
		},
		{
			name:      "should reject a nil question set",
			questions: nil,
			wantErr:   "onesie: at least one question is required",
		},
		{
			name: "should reject a score with one level",
			questions: onesie.Questions{
				{ID: "severity", Question: onesie.Score{Criteria: onesie.Levels("Only one")}},
			},
			wantErr: `onesie: score question 'severity' has 1 criteria, at least two are required`,
		},
		{
			name:      "should reject a score with no levels",
			questions: onesie.Questions{{ID: "severity", Question: onesie.Score{}}},
			wantErr:   `onesie: score question 'severity' has 0 criteria, at least two are required`,
		},
		{
			name: "should reject a score behind a pointer",
			questions: onesie.Questions{
				{ID: "severity", Question: &onesie.Score{Criteria: onesie.Levels("Only one")}},
			},
			wantErr: `onesie: score question 'severity' has 1 criteria, at least two are required`,
		},
		{
			name: "should name the first offender in slice order",
			questions: onesie.Questions{
				{ID: "zebra", Question: onesie.Score{Criteria: onesie.Levels("One")}},
				{ID: "alpha", Question: onesie.Score{Criteria: onesie.Levels("One")}},
			},
			wantErr: `onesie: score question 'zebra' has 1 criteria, at least two are required`,
		},
		{
			name: "should reject a duplicate question id",
			questions: onesie.Questions{
				{ID: "a", Question: onesie.Noul{Instructions: "one"}},
				{ID: "a", Question: onesie.Noul{Instructions: "two"}},
			},
			wantErr: `onesie: duplicate question id 'a'`,
		},
		{
			name: "should drop control characters from a repeated id",
			questions: onesie.Questions{
				{ID: "a\nb", Question: onesie.Noul{Instructions: "one"}},
				{ID: "a\nb", Question: onesie.Noul{Instructions: "two"}},
			},
			wantErr: `onesie: duplicate question id 'ab'`,
		},
		{
			name:      "should reject a nil question",
			questions: onesie.Questions{{ID: "a", Question: nil}},
			wantErr:   `onesie: question 'a' must not be nil`,
		},
		{
			name:      "should reject a typed nil question",
			questions: onesie.Questions{{ID: "a", Question: (*onesie.Score)(nil)}},
			wantErr:   `onesie: question 'a' must not be nil`,
		},
		{
			name: "should accept a valid mixed set",
			questions: onesie.Questions{
				{ID: "urgent", Question: onesie.Noul{Instructions: "Urgent?"}},
				{
					ID: "team",
					Question: onesie.Choice{
						Instructions: "Which?",
						Criteria:     onesie.Criteria{{Name: "a"}, {Name: "b"}},
					},
				},
				{
					ID: "severity",
					Question: onesie.Score{
						Instructions: "How bad?",
						Criteria:     onesie.Levels("Low", "High"),
					},
				},
			},
		},
		{
			name:      "should accept a choice at the options limit",
			questions: onesie.Questions{{ID: "pick", Question: onesie.Choice{Criteria: options(onesie.MaxChoiceOptions)}}},
		},
		{
			name:      "should reject a choice over the options limit",
			questions: onesie.Questions{{ID: "pick", Question: onesie.Choice{Criteria: options(onesie.MaxChoiceOptions + 1)}}},
			wantErr:   `onesie: choice question 'pick' has 256 options, and at most 255 are allowed`,
		},
		{
			name:      "should count a repeated option name once, as the body keeps one",
			questions: onesie.Questions{{ID: "pick", Question: onesie.Choice{Criteria: append(options(onesie.MaxChoiceOptions), onesie.NamedCriterion{Name: "option 0"})}}},
		},
		{
			name:      "should accept a score at the levels limit",
			questions: onesie.Questions{{ID: "rate", Question: onesie.Score{Criteria: levels(onesie.MaxScoreLevels)}}},
		},
		{
			name:      "should reject a score over the levels limit",
			questions: onesie.Questions{{ID: "rate", Question: onesie.Score{Criteria: levels(onesie.MaxScoreLevels + 1)}}},
			wantErr:   `onesie: score question 'rate' has 11 levels, and at most 10 are allowed`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := onesie.ValidateQuestions(tc.questions)

			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}

			if err == nil {
				t.Fatalf("expected an error, got none")
			}

			if err.Error() != tc.wantErr {
				t.Errorf("\n got: %s\nwant: %s", err.Error(), tc.wantErr)
			}

			if !errors.Is(err, onesie.ErrValidation) {
				t.Errorf("a validation failure should match ErrValidation")
			}
		})
	}

	t.Run("should name the offending question", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			questions onesie.Questions
			want      string
		}{
			{
				name: "should carry the offending question id when a score is short",
				questions: onesie.Questions{
					{ID: "severity", Question: onesie.Score{Criteria: onesie.Levels("One")}},
				},
				want: "severity",
			},
			{
				name: "should carry the offending question id when an id repeats",
				questions: onesie.Questions{
					{ID: "team", Question: onesie.Noul{Instructions: "one"}},
					{ID: "team", Question: onesie.Noul{Instructions: "two"}},
				},
				want: "team",
			},
			{
				name:      "should carry the offending question id when a question is nil",
				questions: onesie.Questions{{ID: "urgent", Question: nil}},
				want:      "urgent",
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				err := onesie.ValidateQuestions(tc.questions)

				var invalid *onesie.ValidationError
				if !errors.As(err, &invalid) {
					t.Fatalf("expected a *ValidationError, got %T", err)
				}

				if invalid.Question != tc.want {
					t.Errorf("question got %q, want %q", invalid.Question, tc.want)
				}
			})
		}
	})
}

func options(n int) onesie.Criteria {
	criteria := make(onesie.Criteria, n)
	for i := range criteria {
		criteria[i] = onesie.NamedCriterion{Name: "option " + strconv.Itoa(i)}
	}

	return criteria
}

func levels(n int) []any {
	rubric := make([]any, n)
	for i := range rubric {
		rubric[i] = "level " + strconv.Itoa(i)
	}

	return rubric
}

func TestLevels(t *testing.T) {
	t.Parallel()

	t.Run("should widen strings into score criteria", func(t *testing.T) {
		t.Parallel()

		got := onesie.Levels("Low", "High")

		if len(got) != 2 || got[0] != "Low" || got[1] != "High" {
			t.Errorf("got %#v", got)
		}
	})

	t.Run("should return an empty slice for no levels", func(t *testing.T) {
		t.Parallel()

		if got := onesie.Levels(); len(got) != 0 {
			t.Errorf("got %#v, want empty", got)
		}
	})
}

func TestChoice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		choice onesie.Choice
		want   string
	}{
		{
			name: "should marshal criteria in slice order",
			choice: onesie.Choice{
				Instructions: "pick one",
				Criteria: onesie.Criteria{
					{Name: "zebra", Desc: "last alphabetically"},
					{Name: "alpha", Desc: "first alphabetically"},
				},
			},
			want: `{"type":"choice","instructions":"pick one","criteria":` +
				`{"zebra":"last alphabetically","alpha":"first alphabetically"}}`,
		},
		{
			name:   "should marshal absent criteria as null",
			choice: onesie.Choice{Instructions: "q"},
			want:   `{"type":"choice","instructions":"q","criteria":null}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(tc.choice)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			if string(got) != tc.want {
				t.Errorf("Marshal = %s, want %s", got, tc.want)
			}
		})
	}
}
