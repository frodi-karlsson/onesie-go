package onesie_test

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/frodi-karlsson/onesie-go"
)

var errWriteFailed = errors.New("write failed")

func TestProviderPrepareRaw(t *testing.T) {
	t.Parallel()

	const asset = `"assets":{"r":{"format":"png","data_base64":"AQ=="}}`

	longContext := []onesie.RequestOption{onesie.WithLongContext()}

	tests := []struct {
		name     string
		provider onesie.Provider
		body     string
		model    string
		opts     []onesie.RequestOption
		want     string
		wantErr  string
	}{
		{name: "should put the default model first in a body that names none", body: `{"state":"s","questions":{}}`, model: "jev-latest", want: `{"model":"jev-latest","state":"s","questions":{}}`},
		{name: "should fill an empty object", body: ` {} `, model: "jev-latest", want: `{"model":"jev-latest"}`},
		{name: "should fill an empty model in place", body: `{"state":"s","model":"","questions":{}}`, model: "jev-latest", want: `{"state":"s","model":"jev-latest","questions":{}}`},
		{name: "should fill a null model in place", body: `{"state":"s","model":null}`, model: "jev-latest", want: `{"state":"s","model":"jev-latest"}`},
		{name: "should fill a blank model with the provider's own default", provider: onesie.Levanto(), body: `{"state":"s"}`, want: `{"model":"sage-latest","state":"s"}`},
		{name: "should leave a body that names a model byte for byte", body: "{\n  \"model\": \"m\",\n  \"state\": 1.50\n}", model: "jev-latest", want: "{\n  \"model\": \"m\",\n  \"state\": 1.50\n}"},
		{
			name: "should keep a large integer and an out of range number digit for digit", model: "m",
			body: `{"state":{"id":123456789012345678901234567890,"big":1e400,"small":-0.10000000000000000001}}`,
			want: `{"model":"m","state":{"id":123456789012345678901234567890,"big":1e400,"small":-0.10000000000000000001}}`,
		},
		{name: "should keep the last of repeated keys where it stands", body: `{"model":"","state":"s","model":"m"}`, model: "jev-latest", want: `{"state":"s","model":"m"}`},
		{name: "should fill a model whose last key is blank", body: `{"model":"m","state":"s","model":null}`, model: "jev-latest", want: `{"state":"s","model":"jev-latest"}`},
		{name: "should refuse a model that is a number", body: `{"model":5}`, model: "jev-latest", wantErr: "onesie: model must be a string, got a number"},
		{name: "should refuse a model that is an object", body: `{"state":"s","model":{"name":"m"}}`, model: "jev-latest", wantErr: "onesie: model must be a string, got an object"},
		{name: "should refuse a last model that is not a string", body: `{"model":"m","model":true}`, wantErr: "onesie: model must be a string, got a boolean"},
		{name: "should append long context for WithLongContext", body: `{"model":"m","state":"s"}`, opts: longContext, want: `{"model":"m","state":"s","long_context":true}`},
		{name: "should turn a body's false long context true in place", body: `{"model":"m","long_context":false,"state":"s"}`, opts: longContext, want: `{"model":"m","long_context":true,"state":"s"}`},
		{name: "should keep the last of repeated long context keys", body: `{"long_context":true,"model":"m","long_context":false}`, want: `{"model":"m","long_context":false}`},
		{name: "should keep a body's own long context without the option", body: `{"model":"m","long_context":true}`, want: `{"model":"m","long_context":true}`},
		{name: "should refuse a long context that is not true or false", body: `{"model":"m","long_context":"yes"}`, wantErr: "onesie: long_context must be true or false"},
		{
			name: "should show a body as written", body: "{\n  \"model\": \"m\",\n  \"state\": [ 1 , 2 ]\n}",
			want: "{\n  \"model\": \"m\",\n  \"state\": [ 1 , 2 ]\n}",
		},
		{
			name: "should refuse what the provider refuses while encoding", provider: onesie.Cloudflare(),
			body:    `{"model":"clef","state":"r","questions":{},` + asset + `,"images":[]}`,
			wantErr: "onesie: a body for clef carries assets or images, not both",
		},
		{
			name: "should refuse images the provider does not take", provider: onesie.Levanto(),
			body:    `{"state":"r","questions":{},` + asset + `}`,
			wantErr: "onesie: the levanto provider takes no images. The typesafe, openrouter and cloudflare providers do",
		},
		{
			name:    "should refuse an image a send refuses",
			body:    `{"state":"r","questions":{},"assets":{"r":{"format":"gif","data_base64":"AQ=="}}}`,
			wantErr: "onesie: image 'r' has format 'gif', and an image must be png or jpg",
		},
		{
			name: "should refuse a top level key the route does not take", body: `{"model":"m","state":"s","stat":"t"}`,
			wantErr: "onesie: the request body has an unknown key 'stat'. A body for m on typesafe takes state, model, questions, long_context and assets",
		},
		{
			name: "should refuse clef's images on a route that takes assets", body: `{"model":"m","state":"s","images":[]}`,
			wantErr: "onesie: the request body has an unknown key 'images'. A body for m on typesafe takes state, model, questions, long_context and assets",
		},
		{
			name: "should take the keys openrouter documents on openrouter", provider: onesie.OpenRouter(),
			body: `{"model":"m","state":"s","provider":{"order":["a"]},"session_id":"x","trace":{"k":1},"user":"u"}`,
			want: `{"model":"m","state":"s","provider":{"order":["a"]},"session_id":"x","trace":{"k":1},"user":"u"}`,
		},
		{
			name: "should name openrouter's keys when one is misspelt", provider: onesie.OpenRouter(), body: `{"model":"m","state":"s","sesion_id":"x"}`,
			wantErr: "onesie: the request body has an unknown key 'sesion_id'. A body for m on openrouter takes state, model, questions, " +
				"long_context, assets, provider, session_id, trace and user",
		},
		{
			name: "should take images on a clef model", provider: onesie.Cloudflare(),
			body: `{"model":"clef-flash","state":"s","images":["data:image/png;base64,AQ=="]}`,
			want: `{"model":"clef-flash","state":"s","images":["data:image/png;base64,AQ=="]}`,
		},
		{
			name: "should refuse images on cloudflare's jev, which takes assets", provider: onesie.Cloudflare(), body: `{"state":"s","images":[]}`,
			wantErr: "onesie: the request body has an unknown key 'images'. A body for typesafe/jev on cloudflare takes state, model, questions and assets",
		},
		{
			name: "should name only what berget serves, which takes no images", provider: onesie.Berget(), body: `{"model":"m","state":"s","extra":1}`,
			wantErr: "onesie: the request body has an unknown key 'extra'. A body for m on berget takes state, model, questions and long_context",
		},
		{
			name: "should name images and not long context on a clef model", provider: onesie.Cloudflare(), body: `{"model":"clef-flash","state":"s","extra":1}`,
			wantErr: "onesie: the request body has an unknown key 'extra'. A body for clef-flash on cloudflare takes state, model, questions, assets and images",
		},
		{
			name: "should still pass a false long context on a route that takes none", provider: onesie.Cloudflare(), body: `{"state":"s","long_context":false}`,
			want: `{"model":"typesafe/jev","state":"s","long_context":false}`,
		},
		{
			name: "should refuse long context the provider does not take", provider: onesie.Cloudflare(), body: `{"state":"s"}`, opts: longContext,
			wantErr: "onesie: the cloudflare provider takes no long context. The typesafe, openrouter, berget and levanto providers do",
		},
		{
			name:    "should refuse a raw score over the levels limit",
			body:    `{"state":"s","questions":{"rate":{"type":"score","instructions":"x","criteria":["0","1","2","3","4","5","6","7","8","9","10"]}}}`,
			wantErr: `onesie: score question 'rate' has 11 levels, and at most 10 are allowed`,
		},
		{
			name: "should take a raw score at the levels limit",
			body: `{"model":"m","questions":{"rate":{"type":"score","criteria":["0","1","2","3","4","5","6","7","8","9"]}}}`,
			want: `{"model":"m","questions":{"rate":{"type":"score","criteria":["0","1","2","3","4","5","6","7","8","9"]}}}`,
		},
		{
			name:    "should refuse a raw choice over the options limit",
			body:    `{"state":"s","questions":{"pick":{"type":"choice","criteria":` + manyOptions(256) + `}}}`,
			wantErr: `onesie: choice question 'pick' has 256 options, and at most 255 are allowed`,
		},
		{
			name: "should name the first question over a limit in body order, not by id",
			body: `{"state":"s","questions":{"zeta":{"type":"score","criteria":["0","1","2","3","4","5","6","7","8","9","10"]},` +
				`"alpha":{"type":"choice","criteria":` + manyOptions(256) + `}}}`,
			wantErr: `onesie: score question 'zeta' has 11 levels, and at most 10 are allowed`,
		},
		{
			name: "should count a repeated raw option name once",
			body: `{"model":"m","questions":{"pick":{"type":"choice","criteria":` + manyOptions(255)[:len(manyOptions(255))-1] + `,"option 0":"y"}}}}`,
			want: `{"model":"m","questions":{"pick":{"type":"choice","criteria":` + manyOptions(255)[:len(manyOptions(255))-1] + `,"option 0":"y"}}}}`,
		},
		{
			name:    "should refuse a question of an unknown type",
			body:    `{"state":"s","questions":{"q":{"type":"tarot"}}}`,
			wantErr: "onesie: question 'q' has the type 'tarot', which is not noul, choice, score or box",
		},
		{
			name:    "should refuse a question key the wire format does not name",
			body:    `{"state":"s","questions":{"q":{"type":"noul","instructions":"i","rubric":{"true":"y"}}}}`,
			wantErr: "onesie: question 'q' has an unknown key 'rubric'. A question takes type, instructions and criteria",
		},
		{
			name:    "should refuse choice criteria that are not an object",
			body:    `{"state":"s","questions":{"t":{"type":"choice","criteria":["a","b"]}}}`,
			wantErr: "onesie: choice question 't' needs criteria as an object",
		},
		{name: "should refuse a failing option", body: `{}`, opts: []onesie.RequestOption{onesie.WithRequestAttemptTimeout(0)}, wantErr: "onesie: attempt timeout must be positive"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			provider := tc.provider
			if provider.Name() == "" {
				provider = onesie.TypeSafe()
			}

			got, err := provider.PrepareRaw(json.RawMessage(tc.body), tc.model, tc.opts...)
			if tc.wantErr != "" {
				if !errors.Is(err, onesie.ErrValidation) || err.Error() != tc.wantErr {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}

				return
			}

			if err != nil || string(got.Bytes()) != tc.want {
				t.Errorf("PrepareRaw = %+v, %v, want %s", got, err, tc.want)
			}
		})
	}

	t.Run("should refuse a body that is not JSON, with the syntax error behind it", func(t *testing.T) {
		t.Parallel()

		_, err := onesie.TypeSafe().PrepareRaw(json.RawMessage(`{"state":1,`), "")

		var syntax *jsontext.SyntacticError
		if !errors.Is(err, onesie.ErrValidation) || !strings.HasPrefix(err.Error(), "onesie: the request body is not JSON: ") || !errors.As(err, &syntax) {
			t.Errorf("error = %v, want a refusal that the body is not JSON, from a syntax error", err)
		}
	})

	t.Run("should change nothing in a prepared body", func(t *testing.T) {
		t.Parallel()

		for _, body := range []string{`{"model":"","state":"s","model":"m","long_context":false}`, `{"state":"s"}`} {
			once, err := onesie.TypeSafe().PrepareRaw(json.RawMessage(body), "", longContext...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			twice, err := onesie.TypeSafe().PrepareRaw(once.Bytes(), "", longContext...)
			if err != nil || string(twice.Bytes()) != string(once.Bytes()) {
				t.Errorf("PrepareRaw twice = %s, %v, want %s", twice.Bytes(), err, once.Bytes())
			}
		}
	})

	t.Run("should fill the default model a client passes", func(t *testing.T) {
		t.Parallel()

		client, _ := newTestClient(t, "https://api.example", onesie.WithDefaultModel("custom"))

		got, err := onesie.TypeSafe().PrepareRaw(json.RawMessage(`{"state":"s"}`), client.DefaultModel())
		if err != nil || string(got.Bytes()) != `{"model":"custom","state":"s"}` {
			t.Errorf("PrepareRaw = %+v, %v, want the client's model", got, err)
		}
	})

	t.Run("should hand out a copy of the body", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{"model":"m","state":"s"}`)

		prepared, err := onesie.TypeSafe().PrepareRaw(body, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		body[len(`{"model":"`)] = 'x'
		printed := prepared.Bytes()
		printed[0] = '['

		if got := string(prepared.Bytes()); got != `{"model":"m","state":"s"}` {
			t.Errorf("Bytes = %s, want the body as it was prepared", got)
		}
	})

	facts := []struct {
		name          string
		body          string
		opts          []onesie.RequestOption
		model         string
		longContext   bool
		box           bool
		questionCount int
	}{
		{name: "should report the default model for a body that names none", body: `{"state":"s"}`, model: "jev-latest"},
		{name: "should report the body's trimmed model", body: `{"model":" jev-1.13.0 "}`, model: "jev-1.13.0"},
		{name: "should report long context from the body", body: `{"long_context":true}`, model: "jev-latest", longContext: true},
		{name: "should report long context from WithLongContext", body: `{}`, opts: longContext, model: "jev-latest", longContext: true},
		{
			name: "should report a box question", model: "jev-latest", box: true, questionCount: 1,
			body: `{"state":"r","questions":{"b":{"type":"box"}},"assets":{"r":{"format":"png","data_base64":"AQ=="}}}`,
		},
		{
			name: "should count each question id once and read the last of a repeated one", model: "jev-latest", box: true, questionCount: 2,
			body: `{"questions":{"a":{"type":"noul"},"b":{"type":"noul"},"a":{"type":"box"}}}`,
		},
		{name: "should count no questions in a member that is not an object", body: `{"questions":[1]}`, model: "jev-latest"},
	}

	for _, tc := range facts {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := onesie.TypeSafe().PrepareRaw(json.RawMessage(tc.body), "", tc.opts...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.Model() != tc.model || got.LongContext() != tc.longContext || got.Box() != tc.box || got.QuestionCount() != tc.questionCount {
				t.Errorf("facts = %q, %v, %v, %d, want %q, %v, %v, %d", got.Model(), got.LongContext(), got.Box(), got.QuestionCount(),
					tc.model, tc.longContext, tc.box, tc.questionCount)
			}
		})
	}

	t.Run("should refuse a zero provider", func(t *testing.T) {
		t.Parallel()

		var zero onesie.Provider

		if _, err := zero.PrepareRaw(json.RawMessage(`{}`), ""); !errors.Is(err, onesie.ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}
	})

	t.Run("should match ErrQuestionLimit for a raw body over a count limit, as for a typed request", func(t *testing.T) {
		t.Parallel()

		body := `{"state":"s","questions":{"rate":{"type":"score","criteria":["0","1","2","3","4","5","6","7","8","9","10"]}}}`
		if _, err := onesie.TypeSafe().PrepareRaw([]byte(body), "m"); !errors.Is(err, onesie.ErrQuestionLimit) {
			t.Errorf("PrepareRaw error = %v, want ErrQuestionLimit", err)
		}
	})
}

func manyOptions(n int) string {
	names := make([]string, n)
	for i := range names {
		names[i] = `"option ` + strconv.Itoa(i) + `":"x"`
	}

	return "{" + strings.Join(names, ",") + "}"
}

func TestPreparedBodyCheckResult(t *testing.T) {
	t.Parallel()

	choice := onesie.Questions{{ID: "pick", Question: onesie.Choice{Instructions: "Which?", Criteria: onesie.Criteria{{Name: "a"}, {Name: "b"}}}}}

	prepared, err := onesie.TypeSafe().Prepare(onesie.Request{State: "s", Questions: choice})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		answers   string
		wantFault bool
	}{
		{name: "should return nil for a result that answers its questions", answers: `{"pick":{"type":"choice","choice":"a","probabilities":{"a":1,"b":0}}}`},
		{name: "should return an AnswerFault for a choice outside its options", answers: `{"pick":{"type":"choice","choice":"c","probabilities":{"c":1}}}`, wantFault: true},
		{name: "should return an AnswerFault for a question left unanswered", answers: `{}`, wantFault: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var result onesie.Result
			if err := json.Unmarshal([]byte(`{"answers":`+tc.answers+`}`), &result); err != nil {
				t.Fatal(err)
			}

			var fault *onesie.AnswerFault

			err := prepared.CheckResult(&result)
			if errors.As(err, &fault) != tc.wantFault || (err != nil) != tc.wantFault {
				t.Errorf("CheckResult = %v, want a fault %t", err, tc.wantFault)
			}
		})
	}
}

func TestPreparedBodyWriteTo(t *testing.T) {
	t.Parallel()

	prepared, err := onesie.TypeSafe().PrepareRaw(json.RawMessage("{ \"state\": \"s\", \"model\": \"m\" }"), "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("should write the bytes Bytes returns", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		n, err := prepared.WriteTo(&out)
		if err != nil || n != int64(out.Len()) || !bytes.Equal(out.Bytes(), prepared.Bytes()) {
			t.Errorf("WriteTo = %d, %v, wrote %s, want %s", n, err, out.Bytes(), prepared.Bytes())
		}
	})

	t.Run("should report a writer that takes fewer bytes as a short write", func(t *testing.T) {
		t.Parallel()

		if _, err := prepared.WriteTo(shortWriter{}); !errors.Is(err, io.ErrShortWrite) {
			t.Errorf("WriteTo error = %v, want %v", err, io.ErrShortWrite)
		}
	})

	t.Run("should return the writer's error", func(t *testing.T) {
		t.Parallel()

		if _, err := prepared.WriteTo(failingWriter{}); !errors.Is(err, errWriteFailed) {
			t.Errorf("WriteTo error = %v, want %v", err, errWriteFailed)
		}
	})
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errWriteFailed
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	return len(p) / 2, nil
}
