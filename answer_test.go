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
		if err == nil || !strings.HasPrefix(err.Error(), `answer "x": `) {
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
		{name: "should refuse a noul answer above 1", answer: `{"type":"noul","noul":1.5}`, refuse: true},
		{name: "should refuse a noul answer below 0", answer: `{"type":"noul","noul":-0.1}`, refuse: true},
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
			name:   "should refuse a choice answer whose choice is not among its probabilities",
			answer: `{"type":"choice","choice":"c","probabilities":{"a":0.8,"b":0.2}}`, refuse: true,
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
			name:   "should refuse a choice answer with a probability above 1",
			answer: `{"type":"choice","choice":"a","probabilities":{"a":1.2,"b":0.2}}`, refuse: true,
		},
		{
			name:   "should refuse a choice answer with a probability below 0",
			answer: `{"type":"choice","choice":"a","probabilities":{"a":0.8,"b":-0.2}}`, refuse: true,
		},
		{
			name:   "should refuse a choice answer whose confidence is above 1",
			answer: `{"type":"choice","choice":"a","probabilities":{"a":0.8,"b":0.2},"confidence":1.5}`, refuse: true,
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
			name:   "should refuse a score answer below the lowest level",
			answer: `{"type":"score","score":-1,"probabilities":{"0":0.5,"1":0.5}}`, refuse: true,
		},
		{
			name:   "should refuse a score answer above the highest level",
			answer: `{"type":"score","score":2.5,"probabilities":{"0":0.5,"1":0.5}}`, refuse: true,
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
			name:   "should refuse a score answer with a probability above 1",
			answer: `{"type":"score","score":1,"probabilities":{"0":0,"1":2}}`, refuse: true,
		},
		{
			name:   "should refuse a score answer whose confidence is below 0",
			answer: `{"type":"score","score":1,"probabilities":{"0":0,"1":1},"confidence":-0.5}`, refuse: true,
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
			name:   "should refuse a box answer whose confidence is above 1",
			answer: `{"type":"box","box":{"top":0.25,"bottom":0.82,"left":0.44,"right":0.84},"confidence":3}`, refuse: true,
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
			case tc.refuse && !strings.HasPrefix(err.Error(), `answer "x": `):
				t.Fatalf("error = %v, want one naming the answer like an unknown type", err)
			}
		})
	}

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

		want := `onesie: answer "department" is a choice, not a noul`
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
