package onesie_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/frodi-karlsson/onesie-go"
)

const sampleResult = `{
  "model": "onesie-1.13.0",
  "answers": {
    "is_urgent": {"type": "noul", "noul": 0.95},
    "department": {
      "type": "choice",
      "choice": "billing",
      "probabilities": {"billing": 0.88, "technical": 0.12, "sales": 0.0},
      "confidence": 0.81
    },
    "frustration": {
      "type": "score",
      "score": 1.05,
      "legend": {"0": "Calm", "1": "Frustrated", "2": "Very angry"},
      "probabilities": {"0": 0.0, "1": 0.95, "2": 0.05},
      "confidence": 0.92
    }
  },
  "usage": {"input_tokens": 296, "output_tokens": 20}
}`

func TestResultUnmarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("should decode every answer type in one response", func(t *testing.T) {
		t.Parallel()

		result := loadSample(t)

		if result.Model != "onesie-1.13.0" {
			t.Errorf("model got %q, want %q", result.Model, "onesie-1.13.0")
		}

		if result.Usage.InputTokens != 296 || result.Usage.OutputTokens != 20 {
			t.Errorf("usage got %+v", result.Usage)
		}

		if len(result.Answers) != 3 {
			t.Fatalf("answer count got %d, want 3", len(result.Answers))
		}

		for name, want := range map[string]string{
			"is_urgent": "noul", "department": "choice", "frustration": "score",
		} {
			if got := result.Answers[name].Kind(); got != want {
				t.Errorf("%s kind got %q, want %q", name, got, want)
			}
		}
	})

	t.Run("should decode the usage cost when present", func(t *testing.T) {
		t.Parallel()

		var result onesie.Result

		body := `{"model":"m","answers":{},"usage":{"input_tokens":1,"output_tokens":2,"cost":0.5}}`
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.Usage.Cost == nil || *result.Usage.Cost != 0.5 {
			t.Errorf("cost got %v, want 0.5", result.Usage.Cost)
		}
	})

	t.Run("should leave the cost nil when absent", func(t *testing.T) {
		t.Parallel()

		result := loadSample(t)
		if result.Usage.Cost != nil {
			t.Errorf("cost got %v, want nil", *result.Usage.Cost)
		}
	})

	t.Run("should reject an unknown answer type", func(t *testing.T) {
		t.Parallel()

		body := `{"model":"m","answers":{"x":{"type":"tarot"}},"usage":{}}`

		var result onesie.Result
		if err := json.Unmarshal([]byte(body), &result); err == nil {
			t.Fatalf("expected an error, got none")
		}
	})

	t.Run("should refuse an answer the way it refuses an unknown type", func(t *testing.T) {
		t.Parallel()

		body := `{"model":"m","answers":{"x":{"type":"tarot"}},"usage":{}}`

		var result onesie.Result

		err := json.Unmarshal([]byte(body), &result)
		if err == nil || !strings.HasPrefix(err.Error(), `onesie: answer 'x' `) {
			t.Fatalf("error = %v, want one naming the answer", err)
		}
	})

	strict := []struct {
		name   string
		answer string
		refuse bool
	}{
		{name: "should accept a noul answer", answer: `{"type":"noul","noul":0.95}`},
		{name: "should accept a noul answer of exactly 0", answer: `{"type":"noul","noul":0}`},
		{name: "should accept a noul answer of exactly 1", answer: `{"type":"noul","noul":1}`},
		{name: "should refuse a noul answer that carries only its type", answer: `{"type":"noul"}`, refuse: true},
		{name: "should refuse a noul answer whose noul is null", answer: `{"type":"noul","noul":null}`, refuse: true},
		{name: "should refuse a noul answer whose noul is a string", answer: `{"type":"noul","noul":"0.9"}`, refuse: true},
		{name: "should refuse a noul answer whose noul is a bool", answer: `{"type":"noul","noul":true}`, refuse: true},
		{name: "should refuse a noul answer that sends value instead of noul", answer: `{"type":"noul","value":0.9}`, refuse: true},
		{name: "should refuse a noul answer that sends p instead of noul", answer: `{"type":"noul","p":0.9}`, refuse: true},
		{name: "should leave a noul answer above 1 to the caller", answer: `{"type":"noul","noul":1.5}`},
		{name: "should leave a noul answer below 0 to the caller", answer: `{"type":"noul","noul":-0.1}`},
		{
			name:   "should accept a choice answer",
			answer: `{"type":"choice","choice":"a","probabilities":{"a":0.8,"b":0.2},"confidence":0.7}`,
		},
		{
			name:   "should accept a choice answer with no confidence",
			answer: `{"type":"choice","choice":"a","probabilities":{"a":0.8,"b":0.2}}`,
		},
		{name: "should refuse a choice answer that carries only its type", answer: `{"type":"choice"}`, refuse: true},
		{
			name:   "should refuse a choice answer with no choice",
			answer: `{"type":"choice","probabilities":{"a":0.8,"b":0.2}}`, refuse: true,
		},
		{
			name:   "should refuse a choice answer whose choice is null",
			answer: `{"type":"choice","choice":null,"probabilities":{"a":0.8,"b":0.2}}`, refuse: true,
		},
		{
			name:   "should refuse a choice answer whose choice is a number",
			answer: `{"type":"choice","choice":1,"probabilities":{"a":0.8,"b":0.2}}`, refuse: true,
		},
		{
			name:   "should refuse a choice answer whose choice is empty",
			answer: `{"type":"choice","choice":"","probabilities":{"a":0.8,"b":0.2}}`, refuse: true,
		},
		{
			name:   "should leave a choice that is not among its probabilities to the caller",
			answer: `{"type":"choice","choice":"c","probabilities":{"a":0.8,"b":0.2}}`,
		},
		{name: "should refuse a choice answer with no probabilities", answer: `{"type":"choice","choice":"a"}`, refuse: true},
		{
			name:   "should refuse a choice answer whose probabilities are null",
			answer: `{"type":"choice","choice":"a","probabilities":null}`, refuse: true,
		},
		{
			name:   "should refuse a choice answer whose probabilities are empty",
			answer: `{"type":"choice","choice":"a","probabilities":{}}`, refuse: true,
		},
		{
			name:   "should refuse a choice answer whose probabilities are a list",
			answer: `{"type":"choice","choice":"a","probabilities":[0.8,0.2]}`, refuse: true,
		},
		{
			name:   "should refuse a choice answer with a null probability",
			answer: `{"type":"choice","choice":"a","probabilities":{"a":0.8,"b":null}}`, refuse: true,
		},
		{
			name:   "should refuse a choice answer with a string probability",
			answer: `{"type":"choice","choice":"a","probabilities":{"a":"0.8","b":0.2}}`, refuse: true,
		},
		{
			name:   "should leave a choice probability above 1 to the caller",
			answer: `{"type":"choice","choice":"a","probabilities":{"a":1.2,"b":0.2}}`,
		},
		{
			name:   "should leave a choice probability below 0 to the caller",
			answer: `{"type":"choice","choice":"a","probabilities":{"a":0.8,"b":-0.2}}`,
		},
		{
			name:   "should leave a choice confidence above 1 to the caller",
			answer: `{"type":"choice","choice":"a","probabilities":{"a":0.8,"b":0.2},"confidence":1.5}`,
		},
		{
			name:   "should refuse a choice answer whose confidence is a string",
			answer: `{"type":"choice","choice":"a","probabilities":{"a":0.8,"b":0.2},"confidence":"high"}`, refuse: true,
		},
		{
			name: "should accept a score answer",
			answer: `{"type":"score","score":1.05,"legend":{"0":"Calm","1":"Frustrated","2":"Very angry"},` +
				`"probabilities":{"0":0.0,"1":0.95,"2":0.05},"confidence":0.92}`,
		},
		{
			name:   "should accept a score answer with no legend and no confidence",
			answer: `{"type":"score","score":2,"probabilities":{"0":0,"1":0,"2":1}}`,
		},
		{name: "should refuse a score answer that carries only its type", answer: `{"type":"score"}`, refuse: true},
		{name: "should refuse a score answer with no score", answer: `{"type":"score","probabilities":{"0":1}}`, refuse: true},
		{
			name:   "should refuse a score answer whose score is null",
			answer: `{"type":"score","score":null,"probabilities":{"0":1}}`, refuse: true,
		},
		{
			name:   "should refuse a score answer whose score is a string",
			answer: `{"type":"score","score":"1","probabilities":{"0":0,"1":1}}`, refuse: true,
		},
		{
			name:   "should leave a score below the lowest level to the caller",
			answer: `{"type":"score","score":-1,"probabilities":{"0":0.5,"1":0.5}}`,
		},
		{
			name:   "should leave a score above the highest level to the caller",
			answer: `{"type":"score","score":2.5,"probabilities":{"0":0.5,"1":0.5}}`,
		},
		{
			name:   "should accept a score answer whose probability keys are labels",
			answer: `{"type":"score","score":1.2,"probabilities":{"calm":0.1,"annoyed":0.6,"furious":0.3}}`,
		},
		{name: "should refuse a score answer with no probabilities", answer: `{"type":"score","score":1}`, refuse: true},
		{
			name:   "should refuse a score answer whose probabilities are null",
			answer: `{"type":"score","score":1,"probabilities":null}`, refuse: true,
		},
		{
			name:   "should refuse a score answer whose probabilities are empty",
			answer: `{"type":"score","score":0,"probabilities":{}}`, refuse: true,
		},
		{
			name:   "should refuse a score answer with a null probability",
			answer: `{"type":"score","score":1,"probabilities":{"0":null,"1":1}}`, refuse: true,
		},
		{
			name:   "should leave a score probability above 1 to the caller",
			answer: `{"type":"score","score":1,"probabilities":{"0":0,"1":2}}`,
		},
		{
			name:   "should leave a score confidence below 0 to the caller",
			answer: `{"type":"score","score":1,"probabilities":{"0":0,"1":1},"confidence":-0.5}`,
		},
		{
			name:   "should accept a box answer",
			answer: `{"type":"box","box":{"top":0.25,"bottom":0.82,"left":0.44,"right":0.84},"confidence":0.92}`,
		},
		{
			name:   "should accept a box answer with no confidence",
			answer: `{"type":"box","box":{"top":0.25,"bottom":0.82,"left":0.44,"right":0.84}}`,
		},
		{
			name:   "should accept a box answer whose edges are out of range",
			answer: `{"type":"box","box":{"top":-0.1,"bottom":1.2,"left":0.44,"right":0.84}}`,
		},
		{name: "should refuse a box answer that carries only its type", answer: `{"type":"box"}`, refuse: true},
		{name: "should refuse a box answer whose box is null", answer: `{"type":"box","box":null}`, refuse: true},
		{name: "should refuse a box answer whose box is a list", answer: `{"type":"box","box":[0.1,0.2,0.3,0.4]}`, refuse: true},
		{
			name:   "should refuse a box answer missing an edge",
			answer: `{"type":"box","box":{"top":0.25,"bottom":0.82,"left":0.44}}`, refuse: true,
		},
		{
			name:   "should refuse a box answer with a null edge",
			answer: `{"type":"box","box":{"top":0.25,"bottom":null,"left":0.44,"right":0.84}}`, refuse: true,
		},
		{
			name:   "should refuse a box answer with a string edge",
			answer: `{"type":"box","box":{"top":"0.25","bottom":0.82,"left":0.44,"right":0.84}}`, refuse: true,
		},
		{
			name:   "should leave a box confidence above 1 to the caller",
			answer: `{"type":"box","box":{"top":0.25,"bottom":0.82,"left":0.44,"right":0.84},"confidence":3}`,
		},
	}

	for _, tc := range strict {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := `{"model":"m","answers":{"x":` + tc.answer + `},"usage":{}}`

			var result onesie.Result

			err := json.Unmarshal([]byte(body), &result)

			switch {
			case !tc.refuse && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.refuse && err == nil:
				t.Fatalf("decoded %s as %+v, want it refused", tc.answer, result.Answers["x"])
			case tc.refuse && !strings.HasPrefix(err.Error(), `onesie: answer 'x' `):
				t.Fatalf("error = %v, want one naming the answer like an unknown type", err)
			}
		})
	}

	t.Run("should refuse the whole result when one of several answers has a missing field", func(t *testing.T) {
		t.Parallel()

		body := `{"model":"m","answers":{` +
			`"fine":{"type":"noul","noul":0.9},` +
			`"broken":{"type":"choice","probabilities":{"a":1}},` +
			`"also_fine":{"type":"box","box":{"top":0,"bottom":1,"left":0,"right":1}}},"usage":{}}`

		var result onesie.Result

		err := json.Unmarshal([]byte(body), &result)
		if err == nil || err.Error() != `onesie: answer 'broken' has no choice, or it is null` {
			t.Fatalf("error = %v, want the broken answer named", err)
		}
	})

	t.Run("should name the first null probability in sorted order", func(t *testing.T) {
		t.Parallel()

		body := `{"model":"m","answers":{"x":{"type":"choice","choice":"a",` +
			`"probabilities":{"z":null,"b":null,"a":1}}},"usage":{}}`

		for range 20 {
			var result onesie.Result

			err := json.Unmarshal([]byte(body), &result)
			if err == nil || err.Error() != `onesie: answer 'x' has no probability for 'b', or it is null` {
				t.Fatalf("error = %v, want the null probability of b named", err)
			}
		}
	})

	t.Run("should name the first missing box edge in a fixed order", func(t *testing.T) {
		t.Parallel()

		body := `{"model":"m","answers":{"x":{"type":"box","box":{"top":0.1}}},"usage":{}}`

		for range 20 {
			var result onesie.Result

			err := json.Unmarshal([]byte(body), &result)
			if err == nil || err.Error() != `onesie: answer 'x' has no box bottom edge, or it is null` {
				t.Fatalf("error = %v, want the bottom edge named", err)
			}
		}
	})

	shapes := []struct {
		name string
		body string
		text string
	}{
		{
			name: "should report a body that is an array",
			body: `[1]`,
			text: "the response is not a System One result, since it is an array",
		},
		{
			name: "should report a body with no answers",
			body: `{"model":"m","usage":{}}`,
			text: "the response is not a System One result, since it has no answers",
		},
		{
			name: "should report answers that are null",
			body: `{"model":"m","answers":null}`,
			text: "the response is not a System One result, since it has no answers",
		},
		{
			name: "should report answers that are not an object",
			body: `{"model":"m","answers":[1],"usage":{}}`,
			text: "the response is not a System One result, since its answers are not an object",
		},
		{
			name: "should report a usage count that is not a number",
			body: `{"model":"m","answers":{},"usage":{"input_tokens":"7"}}`,
			text: "the response is not a System One result, since its usage.input_tokens is not a number",
		},
		{
			name: "should report an answer that is not an object",
			body: `{"answers":{"x":[1]}}`,
			text: "answer 'x' is an array, not an object",
		},
		{
			name: "should report an answer that is null",
			body: `{"answers":{"x":null}}`,
			text: "answer 'x' is null, not an object",
		},
		{
			name: "should report an answer with no type",
			body: `{"answers":{"x":{"noul":0.5}}}`,
			text: "answer 'x' has no type, or it is null",
		},
		{
			name: "should report an answer whose type is not a string",
			body: `{"answers":{"x":{"type":5}}}`,
			text: "answer 'x' has a type that is not a string",
		},
		{
			name: "should report an unknown answer type",
			body: `{"answers":{"x":{"type":"tarot"}}}`,
			text: "answer 'x' has the type 'tarot', which is not noul, choice, score or box",
		},
		{
			name: "should report a missing noul",
			body: `{"answers":{"x":{"type":"noul"}}}`,
			text: "answer 'x' has no noul, or it is null",
		},
		{
			name: "should report a noul that is not a number",
			body: `{"answers":{"x":{"type":"noul","noul":"0.9"}}}`,
			text: "answer 'x' has a noul that is not a number",
		},
		{
			name: "should report an empty choice",
			body: `{"answers":{"x":{"type":"choice","choice":"","probabilities":{"a":1}}}}`,
			text: "answer 'x' has a choice that is empty",
		},
		{
			name: "should report empty probabilities",
			body: `{"answers":{"x":{"type":"choice","choice":"a","probabilities":{}}}}`,
			text: "answer 'x' has probabilities that are empty",
		},
		{
			name: "should report probabilities that are a list",
			body: `{"answers":{"x":{"type":"score","score":1,"probabilities":[1]}}}`,
			text: "answer 'x' has probabilities that are not an object",
		},
		{
			name: "should report a probability that is not a number",
			body: `{"answers":{"x":{"type":"choice","choice":"a","probabilities":{"a":"0.8"}}}}`,
			text: "answer 'x' has a probability for 'a' that is not a number",
		},
		{
			name: "should report a confidence that is not a number",
			body: `{"answers":{"x":{"type":"choice","choice":"a","probabilities":{"a":1},"confidence":"high"}}}`,
			text: "answer 'x' has a confidence that is not a number",
		},
		{
			name: "should report a legend entry that is not a string",
			body: `{"answers":{"x":{"type":"score","score":1,"probabilities":{"0":1},"legend":{"0":5}}}}`,
			text: "answer 'x' has a legend entry for '0' that is not a string",
		},
		{
			name: "should report a box edge that is not a number",
			body: `{"answers":{"x":{"type":"box","box":{"top":"0.1","bottom":1,"left":0,"right":1}}}}`,
			text: "answer 'x' has a box top edge that is not a number",
		},
		{
			name: "should report a box that is a list",
			body: `{"answers":{"x":{"type":"box","box":[0.1]}}}`,
			text: "answer 'x' has a box that is not an object",
		},
		{
			name: "should leave a newline in an answer name out of the message",
			body: `{"answers":{"a\nb":{"type":"noul","noul":"1"}}}`,
			text: "answer 'ab' has a noul that is not a number",
		},
		{
			name: "should leave an ANSI escape in a type out of the message",
			body: `{"answers":{"x":{"type":"\u001b[31mred"}}}`,
			text: "answer 'x' has the type '[31mred', which is not noul, choice, score or box",
		},
		{
			name: "should cut a very long type in the message",
			body: `{"answers":{"x":{"type":"` + strings.Repeat("t", 300) + `"}}}`,
			text: "answer 'x' has the type '" + strings.Repeat("t", 200) + "...', which is not noul, choice, score or box",
		},
		{
			name: "should leave a newline in a probability key out of the message",
			body: `{"answers":{"x":{"type":"choice","choice":"a","probabilities":{"a\nb":null}}}}`,
			text: "answer 'x' has no probability for 'ab', or it is null",
		},
		{
			name: "should cut a very long answer name in the message",
			body: `{"answers":{"` + strings.Repeat("n", 300) + `":[1]}}`,
			text: "answer '" + strings.Repeat("n", 200) + "...' is an array, not an object",
		},
	}

	for _, tc := range shapes {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var result onesie.Result

			if err := json.Unmarshal([]byte(tc.body), &result); err == nil || err.Error() != "onesie: "+tc.text {
				t.Errorf("error = %v, want %q", err, tc.text)
			}
		})
	}

	t.Run("should return an AnswerFault naming the answer to blame", func(t *testing.T) {
		t.Parallel()

		for body, want := range map[string]string{`[1]`: "", `{"answers":{"x":{"type":"tarot"}}}`: "x"} {
			var result onesie.Result

			var fault *onesie.AnswerFault
			if err := json.Unmarshal([]byte(body), &result); !errors.As(err, &fault) || fault.Question != want {
				t.Errorf("error = %v for %s, want an AnswerFault naming %q", err, body, want)
			}
		}
	})

	t.Run("should keep the decoder's error as the cause of a wrong JSON type", func(t *testing.T) {
		t.Parallel()

		var result onesie.Result

		err := json.Unmarshal([]byte(`{"answers":{"x":{"type":"noul","noul":"0.9"}}}`), &result)

		var mistyped *json.UnmarshalTypeError
		if !errors.As(err, &mistyped) || mistyped.Field != "noul" {
			t.Errorf("error = %v, want it to unwrap to the decoder's UnmarshalTypeError", err)
		}
	})

	t.Run("should round trip back to the wire shape", func(t *testing.T) {
		t.Parallel()

		encoded, err := json.Marshal(loadSample(t))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var again onesie.Result
		if decodeErr := json.Unmarshal(encoded, &again); decodeErr != nil {
			t.Fatalf("re-decoding failed: %v", decodeErr)
		}

		noul, err := again.Noul("is_urgent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if noul.Noul != 0.95 {
			t.Errorf("noul got %v, want 0.95", noul.Noul)
		}
	})
}

func TestResultCheck(t *testing.T) {
	t.Parallel()

	urgent := onesie.NamedQuestion{ID: "urgent", Question: onesie.Noul{Instructions: "Urgent?"}}
	team := onesie.NamedQuestion{ID: "team", Question: onesie.Choice{Criteria: onesie.Criteria{{Name: "billing"}, {Name: "technical"}}}}
	severity := onesie.NamedQuestion{ID: "severity", Question: onesie.Score{Criteria: onesie.Levels("low", "mid", "high")}}
	bare := onesie.NamedQuestion{ID: "bare", Question: onesie.Score{}}
	mug := onesie.NamedQuestion{ID: "mug", Question: onesie.Box{Instructions: "the mug"}}

	choice := func(picked string, confidence float64, p map[string]float64) onesie.Answer {
		return &onesie.ChoiceAnswer{Choice: picked, Confidence: confidence, Probabilities: p}
	}
	score := func(value, confidence float64, p map[string]float64) onesie.Answer {
		return &onesie.ScoreAnswer{Score: value, Confidence: confidence, Probabilities: p}
	}
	box := func(edges onesie.Edges, confidence float64) onesie.Answer {
		return &onesie.BoxAnswer{Edges: edges, Confidence: confidence}
	}
	even := map[string]float64{"billing": 0.6, "technical": 0.4}
	levels := map[string]float64{"0": 0.2, "1": 0.3, "2": 0.5}
	inside := onesie.Edges{Top: 0.25, Bottom: 0.82, Left: 0.44, Right: 0.84}
	crossed := "box question 'mug' has a left edge not left of its right edge, or a top edge not above its bottom edge"

	tests := []struct {
		name     string
		question onesie.NamedQuestion
		answer   onesie.Answer
		want     string
	}{
		{name: "should accept a noul inside its range", question: urgent, answer: &onesie.NoulAnswer{Noul: 0.92}},
		{name: "should accept a choice that fits", question: team, answer: choice("billing", 0.6, even)},
		{name: "should accept p that two place rounding leaves just off 1", question: onesie.NamedQuestion{
			ID: "team", Question: onesie.Choice{Criteria: onesie.Criteria{{Name: "a"}, {Name: "b"}, {Name: "c"}}},
		}, answer: choice("a", 0.1, map[string]float64{"a": 0.33, "b": 0.33, "c": 0.33})},
		{name: "should accept a score that fits", question: severity, answer: score(1, 0.5, levels)},
		{name: "should accept a box that fits", question: mug, answer: box(inside, 0.92)},
		{name: "should refuse a missing answer", question: urgent, want: "question 'urgent' has no answer"},
		{
			name: "should refuse a choice answer to a noul question", question: urgent, answer: choice("x", 0.5, nil),
			want: "question 'urgent' expects a noul answer, got choice",
		},
		{
			name: "should refuse a noul answer to a choice question", question: team, answer: &onesie.NoulAnswer{Noul: 0.4},
			want: "question 'team' expects a choice answer, got noul",
		},
		{
			name: "should refuse a choice answer to a score question", question: severity, answer: choice("low", 0.5, nil),
			want: "question 'severity' expects a score answer, got choice",
		},
		{
			name: "should refuse a noul above 1", question: urgent, answer: &onesie.NoulAnswer{Noul: 1.5},
			want: "question 'urgent' answered 1.5, which lies outside [0,1]",
		},
		{
			name: "should refuse a negative noul", question: urgent, answer: &onesie.NoulAnswer{Noul: -0.1},
			want: "question 'urgent' answered -0.1, which lies outside [0,1]",
		},
		{
			name: "should refuse a choice that is not an option", question: team, answer: choice("sales", 0.6, even),
			want: "question 'team' picked 'sales', which is not one of its options: billing, technical",
		},
		{
			name: "should refuse a pick before its p", question: team, answer: choice("sales", 2, map[string]float64{"billing": 3}),
			want: "question 'team' picked 'sales', which is not one of its options: billing, technical",
		},
		{
			name: "should keep the spaces around a pick it reports", question: team, answer: choice(" billing ", 0.6, even),
			want: "question 'team' picked ' billing ', which is not one of its options: billing, technical",
		},
		{
			name: "should keep the spaces around a key it reports", question: team,
			answer: choice("billing", 0.6, map[string]float64{"billing": 0.6, "technical": 0.4, " sales ": 0}),
			want:   "question 'team' has p for ' sales ', which is not one of its options: billing, technical",
		},
		{
			name: "should drop control characters from a pick it reports", question: team, answer: choice("x\ty\x00", 0.6, even),
			want: "question 'team' picked 'xy', which is not one of its options: billing, technical",
		},
		{
			name: "should refuse an extra key in p", question: team,
			answer: choice("billing", 0.6, map[string]float64{"billing": 0.6, "technical": 0.4, "sales": 0}),
			want:   "question 'team' has p for 'sales', which is not one of its options: billing, technical",
		},
		{
			name: "should refuse a missing option in p", question: team, answer: choice("billing", 0.6, map[string]float64{"billing": 1}),
			want: "question 'team' has no p for 'technical'",
		},
		{
			name: "should refuse a p outside its range in option order", question: team,
			answer: choice("billing", 0.6, map[string]float64{"billing": 1.1, "technical": -0.1}),
			want:   "question 'team' has p for 'billing' of 1.1, which lies outside [0,1]",
		},
		{
			name: "should refuse p that does not add up to 1", question: team,
			answer: choice("billing", 0.6, map[string]float64{"billing": 0.6, "technical": 0.2}),
			want:   "question 'team' has p that adds up to 0.8, not 1",
		},
		{
			name: "should refuse a confidence above 1", question: team, answer: choice("billing", 1.2, even),
			want: "question 'team' has confidence 1.2, which lies outside [0,1]",
		},
		{
			name: "should refuse a score outside its levels", question: severity, answer: score(3, 0.5, levels),
			want: "question 'severity' has score 3, which lies outside [0,2]",
		},
		{
			name: "should refuse p keyed by label rather than by index", question: severity,
			answer: score(1, 0.5, map[string]float64{"low": 0.2, "mid": 0.3, "high": 0.5}),
			want:   "question 'severity' has p for 'high', which is not a level index from 0 to 2",
		},
		{
			name: "should refuse a missing level in p", question: severity, answer: score(1, 0.5, map[string]float64{"0": 0.5, "1": 0.5}),
			want: "question 'severity' has no p for level 2",
		},
		{
			name: "should refuse a score to a question with no levels", question: bare, answer: score(0, 1, map[string]float64{}),
			want: "question 'bare' has no levels to score against",
		},
		{
			name: "should refuse a score confidence below 0", question: severity, answer: score(1, -0.5, levels),
			want: "question 'severity' has confidence -0.5, which lies outside [0,1]",
		},
		{
			name: "should refuse an edge above 1", question: mug, answer: box(onesie.Edges{Top: 0, Bottom: 1.2, Left: 0, Right: 1}, 0.5),
			want: "box question 'mug' has an edge or a confidence outside [0,1]",
		},
		{
			name: "should refuse a box confidence above 1", question: mug, answer: box(inside, 3),
			want: "box question 'mug' has an edge or a confidence outside [0,1]",
		},
		{
			name: "should refuse a left edge right of the right edge", question: mug,
			answer: box(onesie.Edges{Top: 0, Bottom: 1, Left: 0.6, Right: 0.4}, 0.5), want: crossed,
		},
		{
			name: "should refuse a top edge below the bottom edge", question: mug,
			answer: box(onesie.Edges{Top: 0.9, Bottom: 0.1, Left: 0, Right: 1}, 0.5), want: crossed,
		},
		{
			name: "should check a question behind a pointer", answer: score(3, 0.5, levels),
			question: onesie.NamedQuestion{ID: "severity", Question: &onesie.Score{Criteria: onesie.Levels("low", "mid", "high")}},
			want:     "question 'severity' has score 3, which lies outside [0,2]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := &onesie.Result{Answers: map[string]onesie.Answer{}}
			if tc.answer != nil {
				result.Answers[tc.question.ID] = tc.answer
			}

			err := result.Check(onesie.Questions{tc.question})
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}

			var fault *onesie.AnswerFault
			if !errors.As(err, &fault) || fault.Question != tc.question.ID || err.Error() != "onesie: "+tc.want || fault.Reason() != tc.want {
				t.Fatalf("error = %v, want an AnswerFault for %s reading %q", err, tc.question.ID, tc.want)
			}
		})
	}

	t.Run("should name the first question in question order", func(t *testing.T) {
		t.Parallel()

		result := &onesie.Result{Answers: map[string]onesie.Answer{
			"urgent": &onesie.NoulAnswer{Noul: 2}, "team": choice("sales", 0.6, even),
		}}

		err := result.Check(onesie.Questions{team, urgent})
		if err == nil || err.Error() != "onesie: question 'team' picked 'sales', which is not one of its options: billing, technical" {
			t.Fatalf("error = %v, want the team question named first", err)
		}
	})

	t.Run("should ignore an answer no question asked for", func(t *testing.T) {
		t.Parallel()

		result := &onesie.Result{Answers: map[string]onesie.Answer{
			"urgent": &onesie.NoulAnswer{Noul: 0.5}, "extra": &onesie.NoulAnswer{Noul: 9},
		}}

		if err := result.Check(onesie.Questions{urgent}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestResultNoul(t *testing.T) {
	t.Parallel()

	t.Run("should return the noul answer", func(t *testing.T) {
		t.Parallel()

		got, err := loadSample(t).Noul("is_urgent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got.Noul != 0.95 {
			t.Errorf("got %v, want 0.95", got.Noul)
		}
	})

	t.Run("should report a missing answer", func(t *testing.T) {
		t.Parallel()

		_, err := loadSample(t).Noul("absent")

		var answerErr *onesie.AnswerError
		if !errors.As(err, &answerErr) {
			t.Fatalf("expected an *AnswerError, got %T", err)
		}

		if !answerErr.Missing {
			t.Errorf("Missing should be set for an absent answer")
		}
	})

	t.Run("should report a type mismatch", func(t *testing.T) {
		t.Parallel()

		_, err := loadSample(t).Noul("department")
		if err == nil {
			t.Fatalf("expected an error, got none")
		}

		want := `onesie: answer 'department' is a choice, not a noul`
		if err.Error() != want {
			t.Errorf("\n got: %s\nwant: %s", err.Error(), want)
		}
	})
}

func TestResultChoice(t *testing.T) {
	t.Parallel()

	t.Run("should return the choice answer with a summing distribution", func(t *testing.T) {
		t.Parallel()

		got, err := loadSample(t).Choice("department")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got.Choice != "billing" || got.Confidence != 0.81 {
			t.Errorf("got %+v", got)
		}

		if p, ok := got.Probability("technical"); !ok || p != 0.12 {
			t.Errorf("probability got %v %v, want 0.12 true", p, ok)
		}
	})

	t.Run("should report an unknown option rather than returning zero", func(t *testing.T) {
		t.Parallel()

		got, err := loadSample(t).Choice("department")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, ok := got.Probability("typo"); ok {
			t.Errorf("an unknown option was reported as present")
		}
	})
}

func TestResultScore(t *testing.T) {
	t.Parallel()

	t.Run("should return the score answer with its legend", func(t *testing.T) {
		t.Parallel()

		got, err := loadSample(t).Score("frustration")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got.Score != 1.05 {
			t.Errorf("score got %v, want 1.05", got.Score)
		}

		if got.Legend["1"] != "Frustrated" {
			t.Errorf("legend got %+v", got.Legend)
		}
	})
}

func loadSample(t *testing.T) *onesie.Result {
	t.Helper()

	var result onesie.Result
	if err := json.Unmarshal([]byte(sampleResult), &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return &result
}

func TestResultBox(t *testing.T) {
	t.Parallel()

	const body = `{"model":"jev-1.14.0","answers":{` +
		`"mug":{"type":"box","box":{"top":0.25,"bottom":0.82,"left":0.44,"right":0.84},"confidence":0.92},` +
		`"there":{"type":"noul","noul":0.9}},"usage":{}}`

	var result onesie.Result
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Run("should decode a box answer", func(t *testing.T) {
		t.Parallel()

		got, err := result.Box("mug")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := onesie.BoxAnswer{
			Edges:      onesie.Edges{Top: 0.25, Bottom: 0.82, Left: 0.44, Right: 0.84},
			Confidence: 0.92,
		}
		if *got != want {
			t.Errorf("box = %+v, want %+v", *got, want)
		}
	})

	t.Run("should refuse a noul read as a box", func(t *testing.T) {
		t.Parallel()

		_, err := result.Box("there")

		var answerErr *onesie.AnswerError
		if !errors.As(err, &answerErr) || answerErr.Want != "box" || answerErr.Got != "noul" {
			t.Fatalf("error = %v, want an AnswerError wanting box and getting noul", err)
		}
	})

	t.Run("should marshal back to the wire shape with its type", func(t *testing.T) {
		t.Parallel()

		got, err := json.Marshal(result.Answers["mug"])
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := `{"type":"box","box":{"top":0.25,"bottom":0.82,"left":0.44,"right":0.84},"confidence":0.92}`
		if string(got) != want {
			t.Errorf("json = %s, want %s", got, want)
		}
	})
}
