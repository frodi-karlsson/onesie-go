package onesie_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/frodi-karlsson/onesie-go"
)

var updateEncode = flag.Bool("update-encode", false, "rewrite testdata/encode from the current encoding")

func checkEncodeGolden(t *testing.T, dir, name string, got []byte, err error) {
	t.Helper()

	path := filepath.Join("testdata", "encode", dir, name+".json")
	if err != nil {
		path = filepath.Join("testdata", "encode", dir, name+".error.txt")
		got = []byte(err.Error() + "\n")
	}

	if *updateEncode {
		writeEncodeGolden(t, filepath.Join("testdata", "encode", dir), name, path, got)

		return
	}

	want, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("reading the golden for %s %s: %v, rerun with -update-encode for a change made on purpose", dir, name, readErr)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("%s %s =\n%s\nwant %s:\n%s", dir, name, got, path, want)
	}
}

func writeEncodeGolden(t *testing.T, dir, name, path string, got []byte) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, stale := range []string{name + ".json", name + ".error.txt"} {
		if err := os.Remove(filepath.Join(dir, stale)); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(path, got, 0o600); err != nil {
		t.Fatal(err)
	}
}

func sentPayload(
	t *testing.T, route encodeRoute, req onesie.Request, send func(*onesie.Client, onesie.Request) error,
) ([]byte, error) {
	t.Helper()

	var (
		mu   sync.Mutex
		sent []byte
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading the request body: %v", err)
		}

		mu.Lock()
		sent = body
		mu.Unlock()

		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	if route.model != "" {
		req.Model = route.model
	}

	client, _ := newTestClient(t, server.URL, onesie.WithProvider(route.provider))
	err := send(client, req)

	mu.Lock()
	defer mu.Unlock()

	if sent == nil {
		return nil, err
	}

	return sent, nil
}

func encodeRoutes() []encodeRoute {
	return []encodeRoute{
		{name: "typesafe", provider: onesie.TypeSafe()},
		{name: "openrouter", provider: onesie.OpenRouter()},
		{name: "berget", provider: onesie.Berget()},
		{name: "levanto", provider: onesie.Levanto()},
		{name: "cloudflare", provider: onesie.Cloudflare(), model: "typesafe/jev"},
		{name: "cloudflare-clef", provider: onesie.Cloudflare(), model: "clef"},
	}
}

type encodeRoute struct {
	name     string
	provider onesie.Provider
	model    string
}

func encodeCorpus(t *testing.T) []encodeCase {
	t.Helper()

	square, err := onesie.NewImage(squarePNG(t))
	if err != nil {
		t.Fatalf("building the test image: %v", err)
	}

	urgent := onesie.Questions{{ID: "urgent", Question: onesie.Noul{Instructions: "is this urgent"}}}

	return []encodeCase{
		{name: "noul", req: onesie.Request{State: "the server is down", Questions: urgent}},
		{name: "noul-criteria", req: onesie.Request{State: "s", Questions: onesie.Questions{{ID: "urgent", Question: onesie.Noul{
			Instructions: "is this urgent", Criteria: &onesie.NoulCriteria{True: "it blocks work", False: "it can wait"},
		}}}}},
		{name: "choice", req: onesie.Request{State: "s", Questions: onesie.Questions{{ID: "team", Question: onesie.Choice{
			Instructions: "which team",
			Criteria: onesie.Criteria{
				{Name: "billing", Desc: "money"},
				{Name: "platform", Desc: map[string]any{"owns": []any{"api", "db"}}},
				{Name: "other"},
			},
		}}}}},
		{name: "choice-nil-criteria", req: onesie.Request{State: "s", Questions: onesie.Questions{{ID: "team", Question: onesie.Choice{
			Instructions: "which team",
		}}}}},
		{name: "score", req: onesie.Request{State: "s", Questions: onesie.Questions{{ID: "mood", Question: onesie.Score{
			Instructions: "how cross", Criteria: onesie.Levels("calm", "annoyed", "furious"),
		}}}}},
		{name: "score-structured", req: onesie.Request{State: "s", Questions: onesie.Questions{{ID: "mood", Question: onesie.Score{
			Instructions: "how cross",
			Criteria:     []any{map[string]any{"calm": "no anger"}, map[string]any{"furious": "shouting"}},
		}}}}},
		{name: "score-nil-criteria", req: onesie.Request{State: "s", Questions: onesie.Questions{{ID: "mood", Question: onesie.Score{
			Instructions: "how cross",
		}}}}},
		{name: "box-image", req: onesie.Request{
			State:     map[string]any{"photo": "image-a"},
			Questions: onesie.Questions{{ID: "mug", Question: onesie.Box{Instructions: "the mug"}}},
			Assets:    onesie.Assets{"image-a": square},
		}},
		{name: "noul-image", req: onesie.Request{
			State: map[string]any{"photo": "image-a"}, Questions: urgent, Assets: onesie.Assets{"image-a": square},
		}},
		{name: "mixed-set", req: onesie.Request{State: "s", Questions: onesie.Questions{
			{ID: "urgent", Question: onesie.Noul{Instructions: "is this urgent"}},
			{ID: "team", Question: onesie.Choice{Instructions: "which team", Criteria: onesie.Criteria{{Name: "a"}, {Name: "b"}}}},
			{ID: "mood", Question: onesie.Score{Instructions: "how cross", Criteria: onesie.Levels("calm", "furious")}},
		}}},
		{name: "embedded-question", req: onesie.Request{State: "s", Questions: onesie.Questions{{ID: "urgent", Question: embeddedNoul{
			Noul: onesie.Noul{Instructions: "is this urgent"},
		}}}}},
		{name: "nil-question", req: onesie.Request{State: "s", Questions: onesie.Questions{{ID: "a", Question: nil}}}},
		{name: "typed-nil-question", req: onesie.Request{State: "s", Questions: onesie.Questions{{ID: "a", Question: (*onesie.Noul)(nil)}}}},
		{name: "structured-instructions", req: onesie.Request{State: "s", Questions: onesie.Questions{{ID: "urgent", Question: onesie.Noul{
			Instructions: map[string]any{"rules": tenKeys(), "examples": []any{tenKeys(), "plain"}},
		}}}}},
		{name: "state-many-keys", req: onesie.Request{State: map[string]any{"top": tenKeys(), "list": []any{tenKeys()}}, Questions: urgent}},
		{name: "long-context", req: onesie.Request{State: "s", Questions: urgent, LongContext: true}},
		{name: "html-and-separators", req: onesie.Request{State: "a < b & c > d \u2028 \u2029", Questions: urgent}},
		{name: "non-ascii", req: onesie.Request{State: "héllo 日本 ü", Questions: urgent}},
		{name: "raw-message-html", req: onesie.Request{State: json.RawMessage(`{"a":"<b>&"}`), Questions: urgent}},
		{name: "raw-repeated-names", req: onesie.Request{State: json.RawMessage(`{"photo":"a", "photo":"b"}`), Questions: urgent}},
		{name: "legacy-marshaler", req: onesie.Request{State: legacyMarshaler{}, Questions: urgent}},
		{name: "nil-slice-and-map", req: onesie.Request{
			State: map[string]any{"list": []string(nil), "map": map[string]int(nil)}, Questions: urgent,
		}},
		{name: "nil-bytes", req: onesie.Request{State: map[string]any{"b": []byte(nil)}, Questions: urgent}},
		{name: "byte-array", req: onesie.Request{State: map[string]any{"a": [3]byte{1, 2, 3}}, Questions: urgent}},
		{name: "omitempty-struct", req: onesie.Request{State: omitted{}, Questions: urgent}},
		{name: "invalid-utf8-string", req: onesie.Request{State: "bad \xff byte", Questions: urgent}},
		{name: "invalid-utf8-key", req: onesie.Request{State: map[string]any{"bad \xff": 1}, Questions: urgent}},
		{name: "duration", req: onesie.Request{State: map[string]any{"waited": 90 * time.Second}, Questions: urgent}},
		{name: "string-option", req: onesie.Request{State: stringOption{S: "x"}, Questions: urgent}},
		{name: "conflicting-fields", req: onesie.Request{State: conflictingFields(), Questions: urgent}},
		{name: "unsupported-nan", req: onesie.Request{State: math.NaN(), Questions: urgent}},
		{name: "unsupported-chan", req: onesie.Request{State: map[string]any{"a": make(chan int)}, Questions: urgent}},
		{name: "invalid-utf8-asset-ref", req: onesie.Request{
			State: "s", Questions: urgent, Assets: onesie.Assets{"bad \xff": square},
		}},
		{name: "invalid-utf8-instructions", req: onesie.Request{State: "s", Questions: onesie.Questions{{ID: "urgent", Question: onesie.Noul{Instructions: "is \xff urgent"}}}}},
	}
}

func tenKeys() map[string]any {
	keys := map[string]any{}
	for _, name := range strings.Fields("kilo alpha juliet bravo india charlie hotel delta golf echo foxtrot lima") {
		keys[name] = name + " value"
	}

	return keys
}

type encodeCase struct {
	name string
	req  onesie.Request
}

type embeddedNoul struct {
	onesie.Noul
}

type legacyMarshaler struct{}

func (legacyMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`{"html":"<b>"}`), nil
}

type omitted struct {
	Count int  `json:"count,omitempty"`
	Seen  bool `json:"seen,omitempty"`
}

type stringOption struct {
	S string `json:"s,string"`
}

func conflictingFields() any {
	// Built at run time, since go vet refuses a struct type that embeds two fields with one json tag,
	// which is the conflict this case records.
	built := reflect.StructOf([]reflect.StructField{
		{Name: "Left", Type: reflect.TypeFor[Left](), Anonymous: true},
		{Name: "Right", Type: reflect.TypeFor[Right](), Anonymous: true},
	})

	return reflect.New(built).Elem().Interface()
}

// Left is one half of the conflict conflictingFields builds. reflect.StructOf refuses unexported fields.
type Left struct {
	X int `json:"x"`
}

// Right is the other half of the conflict conflictingFields builds.
type Right struct {
	X int `json:"x"`
}
