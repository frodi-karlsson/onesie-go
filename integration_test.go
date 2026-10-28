//go:build integration

package onesie_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/frodi-karlsson/onesie-go"
)

const (
	urgentState = "Help! My payouts have been failing for 3 days and I am losing customers."
	calmState   = "Thanks, that fixed it! Great service."
)

func TestLiveClientSystemOne(t *testing.T) {
	t.Parallel()

	t.Run("should answer all three question types in one request", func(t *testing.T) {
		t.Parallel()

		client := liveClient(t)

		result, err := client.SystemOne(liveContext(t, time.Minute), onesie.Request{
			State: urgentState,
			Questions: onesie.Questions{
				{
					ID:       "is_urgent",
					Question: onesie.Noul{Instructions: "Does this message convey urgency?"},
				},
				{
					ID: "department",
					Question: onesie.Choice{
						Instructions: "Which team should handle this?",
						Criteria: onesie.Criteria{
							{Name: "billing", Desc: "Payments, invoicing, payouts, refunds"},
							{Name: "technical", Desc: "Bugs, outages, integrations"},
							{Name: "sales", Desc: "Pricing, upgrades, new accounts"},
						},
					},
				},
				{
					ID: "frustration",
					Question: onesie.Score{
						Instructions: "How frustrated is the customer?",
						Criteria:     onesie.Levels("Calm", "Frustrated", "Very angry"),
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.Model == onesie.TypeSafe().DefaultModel() {
			t.Errorf("model should resolve to a versioned id, got the alias %q", result.Model)
		}

		if result.RequestID == "" {
			t.Errorf("no request id came back")
		}

		if result.Usage.InputTokens <= 0 {
			t.Errorf("input tokens got %d, want a positive count", result.Usage.InputTokens)
		}

		urgency, err := result.Noul("is_urgent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// A wide band. The point is the direction, not a calibration assertion.
		if urgency.Noul < 0.7 {
			t.Errorf("urgency got %v, want above 0.7 for an explicitly urgent message", urgency.Noul)
		}

		department, err := result.Choice("department")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(department.Probabilities) != 3 {
			t.Errorf("probability count got %d, want 3", len(department.Probabilities))
		}

		total := 0.0
		for _, p := range department.Probabilities {
			total += p
		}

		if total < 0.99 || total > 1.01 {
			t.Errorf("probabilities sum to %v, want 1", total)
		}

		if _, ok := department.Probability(department.Choice); !ok {
			t.Errorf("the winning option has no probability entry")
		}

		if department.Confidence < 0 || department.Confidence > 1 {
			t.Errorf("confidence got %v, want a value from 0 to 1", department.Confidence)
		}

		frustration, err := result.Score("frustration")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if frustration.Score < 0 || frustration.Score > 2 {
			t.Errorf("score got %v, want a value within the three level rubric", frustration.Score)
		}

		if frustration.Legend["0"] != "Calm" {
			t.Errorf("legend got %+v, want level 0 to echo the criteria", frustration.Legend)
		}

		if len(frustration.Probabilities) != 3 {
			t.Errorf("score probability count got %d, want 3", len(frustration.Probabilities))
		}
	})

	t.Run("should move the answer when the state changes", func(t *testing.T) {
		t.Parallel()

		client := liveClient(t)
		ctx := liveContext(t, time.Minute)

		question := onesie.Questions{
			{ID: "is_urgent", Question: onesie.Noul{Instructions: "Does this message convey urgency?"}},
		}

		hot, err := client.SystemOne(ctx, onesie.Request{State: urgentState, Questions: question})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cold, err := client.SystemOne(ctx, onesie.Request{State: calmState, Questions: question})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		hotValue, err := hot.Noul("is_urgent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		coldValue, err := cold.Noul("is_urgent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if coldValue.Noul >= hotValue.Noul {
			t.Errorf("a calm message scored %v against an urgent %v", coldValue.Noul, hotValue.Noul)
		}

		if coldValue.Noul > 0.3 {
			t.Errorf("calm urgency got %v, want below 0.3", coldValue.Noul)
		}
	})

	t.Run("should accept a structured state and a backtick field reference", func(t *testing.T) {
		t.Parallel()

		client := liveClient(t)

		result, err := client.SystemOne(liveContext(t, time.Minute), onesie.Request{
			State: map[string]any{
				"ticket": map[string]any{
					"messages": []any{
						map[string]any{
							"from": "customer",
							"text": "I was charged twice for order A-104. Please refund the duplicate.",
						},
					},
				},
				"refund_policy": "Duplicate charges are eligible for a refund.",
			},
			Questions: onesie.Questions{
				{
					ID: "refund_requested",
					Question: onesie.Noul{
						Instructions: "Does `ticket.messages[0].text` request a refund?",
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		requested, err := result.Noul("refund_requested")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if requested.Noul < 0.7 {
			t.Errorf("refund requested got %v, want above 0.7", requested.Noul)
		}
	})

	t.Run("should accept a noul with criteria", func(t *testing.T) {
		t.Parallel()

		client := liveClient(t)

		result, err := client.SystemOne(liveContext(t, time.Minute), onesie.Request{
			State: urgentState,
			Questions: onesie.Questions{
				{
					ID: "time_sensitive",
					Question: onesie.Noul{
						Instructions: "Is this time sensitive?",
						Criteria: &onesie.NoulCriteria{
							True:  "The customer states or implies a deadline or ongoing loss",
							False: "No time pressure is expressed",
						},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		answer, err := result.Noul("time_sensitive")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if answer.Noul < 0 || answer.Noul > 1 {
			t.Errorf("noul got %v, want a value from 0 to 1", answer.Noul)
		}
	})

	t.Run("should answer all three question types in request order under openrouter", func(t *testing.T) {
		t.Parallel()

		client := liveOpenRouterClient(t)

		result, err := client.SystemOne(liveContext(t, time.Minute), onesie.Request{
			State: urgentState,
			Questions: onesie.Questions{
				{ID: "z_urgent", Question: onesie.Noul{Instructions: "Does this message convey urgency?"}},
				{
					ID: "a_team",
					Question: onesie.Choice{
						Instructions: "Which team should handle this?",
						Criteria: onesie.Criteria{
							{Name: "billing", Desc: "Payments, invoicing, payouts, refunds"},
							{Name: "technical", Desc: "Bugs, outages, integrations"},
						},
					},
				},
				{
					ID: "m_frustration",
					Question: onesie.Score{
						Instructions: "How frustrated is the customer?",
						Criteria:     onesie.Levels("Calm", "Frustrated", "Very angry"),
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for _, id := range []string{"z_urgent", "a_team", "m_frustration"} {
			if _, ok := result.Answers[id]; !ok {
				t.Errorf("no answer for %s", id)
			}
		}

		if result.RequestID == "" {
			t.Error("the request id is empty, want it from x-generation-id")
		}

		if result.Usage.Cost == nil {
			t.Error("the usage carries no cost")
		}
	})

	t.Run("should answer all three question types in request order under levanto", func(t *testing.T) {
		t.Parallel()

		result, err := liveLevantoClient(t).SystemOne(liveContext(t, time.Minute), onesie.Request{
			State: urgentState,
			Questions: onesie.Questions{
				{ID: "z_urgent", Question: onesie.Noul{Instructions: "Does this message convey urgency?"}},
				{
					ID: "a_team",
					Question: onesie.Choice{
						Instructions: "Which team should handle this?",
						Criteria: onesie.Criteria{
							{Name: "billing", Desc: "Payments, invoicing, payouts, refunds"},
							{Name: "technical", Desc: "Bugs, outages, integrations"},
						},
					},
				},
				{
					ID: "m_frustration",
					Question: onesie.Score{
						Instructions: "How frustrated is the customer?",
						Criteria:     onesie.Levels("Calm", "Frustrated", "Very angry"),
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for _, id := range []string{"z_urgent", "a_team", "m_frustration"} {
			if _, ok := result.Answers[id]; !ok {
				t.Errorf("no answer for %s", id)
			}
		}

		if result.RequestID == "" {
			t.Error("the request id is empty, want it from x-request-id")
		}

		if result.Model == "" {
			t.Error("the response names no model")
		}
	})

	t.Run("should see the red square and not see one on an empty canvas", func(t *testing.T) {
		t.Parallel()

		square, err := onesie.NewImage(squarePNG(t))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		empty, err := onesie.NewImage(emptyPNG(t))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		result, err := liveClient(t).SystemOne(liveContext(t, time.Minute), onesie.Request{
			State: map[string]any{"square": "image-square", "empty": "image-empty"},
			Questions: onesie.Questions{
				{ID: "in_square", Question: onesie.Noul{Instructions: "Is there a red square in `square`?"}},
				{ID: "in_empty", Question: onesie.Noul{Instructions: "Is there a red square in `empty`?"}},
			},
			Assets: onesie.Assets{"image-square": square, "image-empty": empty},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got, _ := result.Noul("in_square"); got == nil || got.Noul < 0.8 {
			t.Errorf("in_square = %+v, want a clear yes", got)
		}

		if got, _ := result.Noul("in_empty"); got == nil || got.Noul > 0.2 {
			t.Errorf("in_empty = %+v, want a clear no", got)
		}
	})

	t.Run("should box the red square on jev-preview", func(t *testing.T) {
		t.Parallel()

		square, err := onesie.NewImage(squarePNG(t))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		result, err := liveClient(t).SystemOne(liveContext(t, time.Minute), onesie.Request{
			State:     map[string]any{"photo": "image-square"},
			Model:     "jev-preview",
			Questions: onesie.Questions{{ID: "square", Question: onesie.Box{Instructions: "the red square in `photo`"}}},
			Assets:    onesie.Assets{"image-square": square},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got, err := result.Box("square")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// The square's edges are 0.25, 0.25, 0.5 and 0.5. Box is experimental, so the tolerance
		// starts loose and is meant to tighten once real answers have been seen.
		const tolerance = 0.05

		want := onesie.Edges{Top: 0.25, Bottom: 0.5, Left: 0.25, Right: 0.5}
		for name, pair := range map[string][2]float64{
			"top": {got.Edges.Top, want.Top}, "bottom": {got.Edges.Bottom, want.Bottom},
			"left": {got.Edges.Left, want.Left}, "right": {got.Edges.Right, want.Right},
		} {
			if math.Abs(pair[0]-pair[1]) > tolerance {
				t.Errorf("%s = %.3f, want %.2f within %.2f. Box: %+v", name, pair[0], pair[1], tolerance, got)
			}
		}
	})

	t.Run("should see the red square through openrouter's inline parts", func(t *testing.T) {
		t.Parallel()

		square, err := onesie.NewImage(squarePNG(t))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		empty, err := onesie.NewImage(emptyPNG(t))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		req := onesie.Request{
			State: map[string]any{"square": "image-square", "empty": "image-empty"},
			Model: "typesafe/jev-1.14",
			Questions: onesie.Questions{
				{ID: "in_square", Question: onesie.Noul{Instructions: "Is there a red square in `square`?"}},
				{ID: "in_empty", Question: onesie.Noul{Instructions: "Is there a red square in `empty`?"}},
				{ID: "box", Question: onesie.Box{Instructions: "the red square in `square`"}},
			},
			Assets: onesie.Assets{"image-square": square, "image-empty": empty},
		}

		client := liveOpenRouterClient(t)

		result, err := client.SystemOne(liveContext(t, time.Minute), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got, _ := result.Noul("in_square"); got == nil || got.Noul < 0.8 {
			t.Errorf("in_square = %+v, want a clear yes", got)
		}

		if got, _ := result.Noul("in_empty"); got == nil || got.Noul > 0.2 {
			t.Errorf("in_empty = %+v, want a clear no", got)
		}

		box, err := result.Box("box")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := onesie.Edges{Top: 0.25, Bottom: 0.5, Left: 0.25, Right: 0.5}
		for name, pair := range map[string][2]float64{
			"top": {box.Edges.Top, want.Top}, "bottom": {box.Edges.Bottom, want.Bottom},
			"left": {box.Edges.Left, want.Left}, "right": {box.Edges.Right, want.Right},
		} {
			if math.Abs(pair[0]-pair[1]) > 0.05 {
				t.Errorf("%s = %.3f, want %.2f within 0.05. Box: %+v", name, pair[0], pair[1], box)
			}
		}

		body, err := onesie.MarshalBody(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		raw, err := client.SystemOneRaw(liveContext(t, time.Minute), body)
		if err != nil {
			t.Fatalf("raw: unexpected error: %v", err)
		}

		var replayed onesie.Result
		if err := json.Unmarshal(raw, &replayed); err != nil {
			t.Fatalf("decoding the raw answer: %v", err)
		}

		if got, _ := replayed.Noul("in_square"); got == nil || got.Noul < 0.8 {
			t.Errorf("raw in_square = %+v, want a clear yes", got)
		}
	})

	t.Run("should accept long context in the request and in a raw body", func(t *testing.T) {
		t.Parallel()

		client := liveClient(t)
		req := onesie.Request{State: urgentState, Questions: onesie.Questions{
			{ID: "urgent", Question: onesie.Noul{Instructions: "Is this urgent?"}},
		}, LongContext: true}

		if _, err := client.SystemOne(liveContext(t, time.Minute), req); err != nil {
			t.Fatalf("SystemOne: unexpected error: %v", err)
		}

		body, err := onesie.MarshalBody(onesie.Request{
			State: urgentState, Model: "jev-latest", Questions: req.Questions, LongContext: true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := client.SystemOneRaw(liveContext(t, time.Minute), body); err != nil {
			t.Fatalf("SystemOneRaw: unexpected error: %v", err)
		}
	})

	t.Run("should send a prepared body and return its checked result", func(t *testing.T) {
		t.Parallel()

		client := liveClient(t)

		body, err := onesie.MarshalBody(onesie.Request{State: urgentState, Questions: onesie.Questions{
			{ID: "urgent", Question: onesie.Noul{Instructions: "Is this urgent?"}},
		}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		prepared, err := client.Provider().PrepareRaw(body, client.DefaultModel())
		if err != nil {
			t.Fatalf("PrepareRaw: unexpected error: %v", err)
		}

		result, raw, err := client.SendPrepared(liveContext(t, time.Minute), prepared)
		if err != nil {
			t.Fatalf("SendPrepared: unexpected error: %v", err)
		}

		if got, _ := result.Noul("urgent"); got == nil || got.Noul < 0.5 || len(raw) == 0 {
			t.Errorf("urgent = %+v with body %s, want a yes and the response body", got, raw)
		}
	})

	t.Run("should answer all three question types through cloudflare's jev", func(t *testing.T) {
		t.Parallel()

		result, err := liveCloudflareClient(t).SystemOne(liveContext(t, time.Minute), onesie.Request{
			State: "I was charged twice for my last invoice and I want the duplicate refunded today!",
			Questions: onesie.Questions{
				{ID: "urgent", Question: onesie.Noul{Instructions: "Is this urgent?"}},
				{ID: "team", Question: onesie.Choice{
					Instructions: "Which team?",
					Criteria:     onesie.Criteria{{Name: "billing", Desc: "payments and refunds"}, {Name: "tech", Desc: "bugs and outages"}},
				}},
				{ID: "mood", Question: onesie.Score{Instructions: "How angry?", Criteria: onesie.Levels("calm", "annoyed", "furious")}},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got, _ := result.Noul("urgent"); got == nil || got.Noul < 0.5 {
			t.Errorf("urgent = %+v, want a yes", got)
		}

		if got, _ := result.Choice("team"); got == nil || got.Choice != "billing" {
			t.Errorf("team = %+v, want billing", got)
		}

		if got, _ := result.Score("mood"); got == nil {
			t.Errorf("mood missing")
		}
	})

	for _, model := range []string{"typesafe/jev", "clef-flash"} {
		t.Run("should see the red square through cloudflare's "+model, func(t *testing.T) {
			t.Parallel()

			square, err := onesie.NewImage(squarePNG(t))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			result, err := liveCloudflareClient(t).SystemOne(liveContext(t, time.Minute), onesie.Request{
				State:     map[string]any{"photo": "image-square"},
				Model:     model,
				Questions: onesie.Questions{{ID: "red", Question: onesie.Noul{Instructions: "Is there a red square in `photo`?"}}},
				Assets:    onesie.Assets{"image-square": square},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got, _ := result.Noul("red"); got == nil || got.Noul < 0.8 {
				t.Errorf("red = %+v, want a clear yes", got)
			}
		})
	}

	for _, redFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("should tell two images apart by name on clef-flash, red named first %v", redFirst), func(t *testing.T) {
			t.Parallel()

			square, err := onesie.NewImage(squarePNG(t))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			empty, err := onesie.NewImage(emptyPNG(t))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			a, b := "image-3f2a9c", "image-9c1d4e"
			assets := onesie.Assets{a: square, b: empty}
			if !redFirst {
				assets = onesie.Assets{a: empty, b: square}
			}

			result, err := liveCloudflareClient(t).SystemOne(liveContext(t, time.Minute), onesie.Request{
				State: map[string]any{"a": a, "b": b},
				Model: "clef-flash",
				Questions: onesie.Questions{
					{ID: "a_red", Question: onesie.Noul{Instructions: "Does `a` show a red square?"}},
					{ID: "b_red", Question: onesie.Noul{Instructions: "Does `b` show a red square?"}},
				},
				Assets: assets,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			aRed, _ := result.Noul("a_red")
			bRed, _ := result.Noul("b_red")

			if aRed == nil || bRed == nil || (aRed.Noul > bRed.Noul) != redFirst {
				t.Errorf("a_red = %+v, b_red = %+v, want the red one to be %v", aRed, bRed, map[bool]string{true: "a", false: "b"}[redFirst])
			}
		})
	}
}

func TestLiveClientListModels(t *testing.T) {
	t.Parallel()

	t.Run("should list at least one named model", func(t *testing.T) {
		t.Parallel()

		client := liveClient(t)

		models, err := client.ListModels(liveContext(t, 30*time.Second))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(models) == 0 {
			t.Fatalf("no models came back")
		}

		for _, model := range models {
			if model.Name == "" {
				t.Errorf("a model came back with no name: %+v", model)
			}
		}
	})

	t.Run("should list at least one named model under openrouter", func(t *testing.T) {
		t.Parallel()

		models, err := liveOpenRouterClient(t).ListModels(liveContext(t, 30*time.Second))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(models) == 0 {
			t.Fatalf("no models came back")
		}

		for _, model := range models {
			if model.Name == "" {
				t.Errorf("a model came back with no name: %+v", model)
			}
		}
	})

	t.Run("should list the models under levanto, the default alias among them", func(t *testing.T) {
		t.Parallel()

		models, err := liveLevantoClient(t).ListModels(liveContext(t, 30*time.Second))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		found := false

		for _, model := range models {
			if model.Name == onesie.Levanto().DefaultModel() {
				found = true
			}
		}

		if !found {
			t.Errorf("no model is named %s: %+v", onesie.Levanto().DefaultModel(), models)
		}
	})

	t.Run("should list jev and clef-flash under cloudflare", func(t *testing.T) {
		t.Parallel()

		models, err := liveCloudflareClient(t).ListModels(liveContext(t, 30*time.Second))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		names := map[string]bool{}
		for _, model := range models {
			names[model.Name] = true
		}

		if !names["typesafe/jev"] || !names["clef-flash"] {
			t.Errorf("models = %+v, want typesafe/jev and clef-flash", models)
		}
	})
}

func TestLiveClientVerifyKey(t *testing.T) {
	t.Parallel()

	t.Run("should accept the configured key under openrouter", func(t *testing.T) {
		t.Parallel()

		models, err := liveOpenRouterClient(t).VerifyKey(liveContext(t, 30*time.Second))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(models) == 0 {
			t.Errorf("no models came back")
		}
	})

	t.Run("should refuse a bad key under openrouter", func(t *testing.T) {
		t.Parallel()

		_ = openRouterKey(t)

		client, err := onesie.New(
			onesie.WithProvider(onesie.OpenRouter()),
			onesie.WithAPIKey("sk-or-v1-definitely-not-a-real-key"),
			onesie.WithUserAgent("onesie-integration"),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = client.VerifyKey(liveContext(t, 30*time.Second))
		if !errors.Is(err, onesie.ErrAuthentication) && !errors.Is(err, onesie.ErrPermissionDenied) {
			t.Fatalf("error got %v, want ErrAuthentication or ErrPermissionDenied", err)
		}
	})
}

func TestLiveAPIError(t *testing.T) {
	t.Parallel()

	t.Run("should return ErrAuthentication for a bad key", func(t *testing.T) {
		t.Parallel()

		// Call apiKey first so this subtest skips alongside the others when no key is configured.
		_ = apiKey(t)

		client, err := onesie.New(
			onesie.WithAPIKey("sk-definitely-not-a-real-key"),
			onesie.WithUserAgent("onesie-integration"),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = client.SystemOne(liveContext(t, 30*time.Second), onesie.Request{
			State:     "x",
			Questions: onesie.Questions{{ID: "q", Question: onesie.Noul{Instructions: "Is this a test?"}}},
		})

		if !errors.Is(err, onesie.ErrAuthentication) {
			t.Fatalf("error got %v, want ErrAuthentication", err)
		}

		var api *onesie.APIError
		if !errors.As(err, &api) {
			t.Fatalf("expected an *APIError, got %T", err)
		}

		if api.Status != 401 {
			t.Errorf("status got %d, want 401", api.Status)
		}
	})

	t.Run("should return an APIError for an unknown model", func(t *testing.T) {
		t.Parallel()

		client := liveClient(t)

		_, err := client.SystemOne(liveContext(t, 30*time.Second), onesie.Request{
			State:     "x",
			Model:     "onesie-does-not-exist",
			Questions: onesie.Questions{{ID: "q", Question: onesie.Noul{Instructions: "Is this a test?"}}},
		})

		var api *onesie.APIError
		if !errors.As(err, &api) {
			t.Fatalf("expected an *APIError, got %v", err)
		}

		if api.Status < 400 || api.Status >= 500 {
			t.Errorf("status got %d, want a 4xx", api.Status)
		}

		if api.Error() == "" {
			t.Errorf("the error carried no message")
		}
	})

	t.Run("should return ErrAuthentication for a bad key under openrouter", func(t *testing.T) {
		t.Parallel()

		_ = openRouterKey(t)

		client, err := onesie.New(
			onesie.WithProvider(onesie.OpenRouter()),
			onesie.WithAPIKey("sk-or-v1-definitely-not-a-real-key"),
			onesie.WithUserAgent("onesie-integration"),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = client.SystemOne(liveContext(t, 30*time.Second), onesie.Request{
			State:     "x",
			Questions: onesie.Questions{{ID: "q", Question: onesie.Noul{Instructions: "Is this a test?"}}},
		})

		if !errors.Is(err, onesie.ErrAuthentication) {
			t.Fatalf("error got %v, want ErrAuthentication", err)
		}
	})

	t.Run("should report an openrouter validation error on one line with its path", func(t *testing.T) {
		t.Parallel()

		body := json.RawMessage(`{"state":"x","model":"jev-latest","questions":` +
			`{"q":{"type":"noul","instructions":"urgent","criteria":{"true":"x","false":null}}}}`)

		_, err := liveOpenRouterClient(t).SystemOneRaw(liveContext(t, 30*time.Second), body)
		if !errors.Is(err, onesie.ErrBadRequest) {
			t.Fatalf("error got %v, want ErrBadRequest", err)
		}

		message := err.Error()
		if strings.Contains(message, "\n") || !strings.Contains(message, "questions.q.criteria.false") {
			t.Errorf("error = %q, want one line naming questions.q.criteria.false", message)
		}
	})

	t.Run("should return ErrAuthentication for a bad key under levanto", func(t *testing.T) {
		t.Parallel()

		_ = levantoKey(t)

		client, err := onesie.New(
			onesie.WithProvider(onesie.Levanto()),
			onesie.WithAPIKey("lv_live_definitely-not-a-real-key"),
			onesie.WithUserAgent("onesie-integration"),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = client.ListModels(liveContext(t, 30*time.Second))
		if !errors.Is(err, onesie.ErrAuthentication) {
			t.Fatalf("error got %v, want ErrAuthentication", err)
		}
	})

	t.Run("should return ErrUnprocessableEntity for jev-latest, which levanto does not serve", func(t *testing.T) {
		t.Parallel()

		_, err := liveLevantoClient(t).SystemOne(liveContext(t, 30*time.Second), onesie.Request{
			State:     "x",
			Model:     onesie.TypeSafe().DefaultModel(),
			Questions: onesie.Questions{{ID: "q", Question: onesie.Noul{Instructions: "Is this a test?"}}},
		})

		if !errors.Is(err, onesie.ErrUnprocessableEntity) || !strings.Contains(err.Error(), "unknown model") {
			t.Fatalf("error got %v, want ErrUnprocessableEntity saying unknown model", err)
		}
	})

	t.Run("should return ErrAuthentication for a bad token under cloudflare", func(t *testing.T) {
		t.Parallel()

		_, account := cloudflareCredentials(t)

		client, err := onesie.New(
			onesie.WithProvider(onesie.Cloudflare()),
			onesie.WithAPIKey("definitely-not-a-real-token"),
			onesie.WithBaseURL("https://api.cloudflare.com/client/v4/accounts/"+account+"/ai"),
			onesie.WithUserAgent("onesie-integration"),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err = client.ListModels(liveContext(t, 30*time.Second)); !errors.Is(err, onesie.ErrAuthentication) {
			t.Fatalf("error got %v, want ErrAuthentication", err)
		}
	})

	t.Run("should name an unknown model under cloudflare", func(t *testing.T) {
		t.Parallel()

		_, err := liveCloudflareClient(t).SystemOne(liveContext(t, 30*time.Second), onesie.Request{
			State:     "x",
			Model:     "typesafe/jev-nope",
			Questions: onesie.Questions{{ID: "q", Question: onesie.Noul{Instructions: "Is this a test?"}}},
		})

		var api *onesie.APIError
		if !errors.As(err, &api) || !strings.Contains(err.Error(), "typesafe/jev-nope") {
			t.Fatalf("error = %v, want an APIError naming the model", err)
		}
	})

	t.Run("should classify each route's refusals and log their bodies", func(t *testing.T) {
		t.Parallel()

		typesafe := func(t *testing.T) *onesie.Client { return liveClient(t) }
		noul := onesie.Questions{{ID: "q", Question: onesie.Noul{Instructions: "Is this urgent?"}}}
		long := strings.Repeat("The payout failed again and the customer is angry about it. ", 40000)

		ask := func(model string, state any) func(*testing.T) onesie.Request {
			return func(*testing.T) onesie.Request {
				return onesie.Request{State: state, Model: model, Questions: noul}
			}
		}

		boxOnLatest := func(t *testing.T) onesie.Request {
			square, err := onesie.NewImage(squarePNG(t))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			return onesie.Request{
				State: map[string]any{"photo": "image-square"}, Model: "jev-latest",
				Questions: onesie.Questions{{ID: "square", Question: onesie.Box{Instructions: "the red square in `photo`"}}},
				Assets:    onesie.Assets{"image-square": square},
			}
		}

		tests := []struct {
			name     string
			fixture  string
			client   func(*testing.T) *onesie.Client
			req      func(*testing.T) onesie.Request
			sentinel error
			mayServe bool
		}{
			{name: "should match ErrUnknownModel for an unknown model on typesafe", fixture: "typesafe-unknown-model", client: typesafe, req: ask("onesie-does-not-exist", "x"), sentinel: onesie.ErrUnknownModel},
			{name: "should match ErrUnknownModel for an unknown model on openrouter", fixture: "openrouter-unknown-model", client: liveOpenRouterClient, req: ask("onesie-does-not-exist", "x"), sentinel: onesie.ErrUnknownModel},
			{name: "should match ErrUnknownModel for an unknown model on berget", fixture: "berget-unknown-model", client: liveBergetClient, req: ask("onesie-does-not-exist", "x"), sentinel: onesie.ErrUnknownModel},
			{name: "should match ErrUnknownModel for an unknown model on levanto", fixture: "levanto-unknown-model", client: liveLevantoClient, req: ask("onesie-does-not-exist", "x"), sentinel: onesie.ErrUnknownModel},
			{name: "should match ErrUnknownModel for an unknown model on the cloudflare gateway", fixture: "gateway-unknown-model", client: liveCloudflareClient, req: ask("typesafe/jev-nope", "x"), sentinel: onesie.ErrUnknownModel},
			{name: "should match ErrUnknownModel for an unknown model on cloudflare workers", fixture: "workers-unknown-model", client: liveCloudflareClient, req: ask("clef-nope", "x"), sentinel: onesie.ErrUnknownModel},
			{name: "should match ErrContextTooLong for a long state on typesafe", fixture: "typesafe-context-too-long", client: typesafe, req: ask("", long), sentinel: onesie.ErrContextTooLong},
			{name: "should match ErrContextTooLong for a long state on openrouter", fixture: "openrouter-context-too-long", client: liveOpenRouterClient, req: ask("", long), sentinel: onesie.ErrContextTooLong},
			{name: "should match ErrContextTooLong for a long state on the cloudflare gateway", fixture: "gateway-context-too-long", client: liveCloudflareClient, req: ask("", long), sentinel: onesie.ErrContextTooLong},
			{name: "should match ErrNotServed for a box on typesafe's latest model", fixture: "typesafe-box-not-served", client: typesafe, req: boxOnLatest, sentinel: onesie.ErrNotServed, mayServe: true},
			{name: "should match ErrNotServed for a box on openrouter's latest model", fixture: "openrouter-box-not-served", client: liveOpenRouterClient, req: boxOnLatest, sentinel: onesie.ErrNotServed, mayServe: true},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				_, err := tc.client(t).SystemOne(liveContext(t, time.Minute), tc.req(t))
				if err == nil && tc.mayServe {
					t.Skip("the model serves box questions, so there is no refusal to match")
				}

				var api *onesie.APIError
				if errors.As(err, &api) {
					t.Logf("fixture %s.json, status %d: %s", tc.fixture, api.Status, api.RawBody)
				}

				if !errors.Is(err, tc.sentinel) {
					t.Errorf("error = %v, want %v", err, tc.sentinel)
				}
			})
		}
	})

	t.Run("should log each route's count refusal past the library's own check", func(t *testing.T) {
		t.Parallel()

		const questions = `"questions":{"rate":{"type":"score","instructions":"How bad?",` +
			`"criteria":["0","1","2","3","4","5","6","7","8","9","10"]}}`

		for _, tc := range []struct {
			fixture  string
			provider onesie.Provider
			target   func(t *testing.T) (url, key, body string)
		}{
			{
				fixture:  "typesafe-question-limit",
				provider: onesie.TypeSafe(),
				target: func(t *testing.T) (string, string, string) {
					return "https://api.typesafe.ai/v1/systemone", apiKey(t), `{"state":"x","model":"jev-latest",` + questions + `}`
				},
			},
			{
				fixture:  "openrouter-question-limit",
				provider: onesie.OpenRouter(),
				target: func(t *testing.T) (string, string, string) {
					return "https://openrouter.ai/api/v1/systemone", openRouterKey(t), `{"state":"x","model":"jev-latest",` + questions + `}`
				},
			},
			{
				fixture:  "gateway-question-limit",
				provider: onesie.Cloudflare(),
				target: func(t *testing.T) (string, string, string) {
					token, account := cloudflareCredentials(t)

					// The gateway takes the model beside the input, never inside it.
					return "https://api.cloudflare.com/client/v4/accounts/" + account + "/ai/run", token,
						`{"model":"typesafe/jev","input":{"state":"x",` + questions + `}}`
				},
			},
		} {
			t.Run("should refuse eleven levels for "+tc.fixture, func(t *testing.T) {
				t.Parallel()

				url, key, body := tc.target(t)

				status, raw := postLive(t, url, key, body)
				t.Logf("fixture %s.json, status %d: %s", tc.fixture, status, raw)

				if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
					t.Errorf("status = %d, want 400 or 422", status)
				}

				if err := replayed(t, tc.provider, status, raw); !errors.Is(err, onesie.ErrQuestionLimit) {
					t.Errorf("error = %v, want ErrQuestionLimit", err)
				}
			})
		}
	})
}

func liveClient(t *testing.T, opts ...onesie.Option) *onesie.Client {
	t.Helper()

	base := []onesie.Option{onesie.WithAPIKey(apiKey(t)), onesie.WithUserAgent("onesie-integration")}

	client, err := onesie.New(append(base, opts...)...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return client
}

func liveOpenRouterClient(t *testing.T) *onesie.Client {
	t.Helper()

	client, err := onesie.New(
		onesie.WithProvider(onesie.OpenRouter()),
		onesie.WithAPIKey(openRouterKey(t)),
		onesie.WithUserAgent("onesie-integration"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return client
}

func liveLevantoClient(t *testing.T) *onesie.Client {
	t.Helper()

	client, err := onesie.New(
		onesie.WithProvider(onesie.Levanto()),
		onesie.WithAPIKey(levantoKey(t)),
		onesie.WithUserAgent("onesie-integration"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return client
}

func liveBergetClient(t *testing.T) *onesie.Client {
	t.Helper()

	client, err := onesie.New(
		onesie.WithProvider(onesie.Berget()),
		onesie.WithAPIKey(bergetKey(t)),
		onesie.WithUserAgent("onesie-integration"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return client
}

func postLive(t *testing.T, url, key, body string) (int, []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(liveContext(t, time.Minute), http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	notFollowing := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	res, err := notFollowing.Do(req)
	if err != nil {
		t.Fatalf("sending the request: %v", err)
	}

	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil {
			t.Logf("closing the body: %v", closeErr)
		}
	}()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}

	return res.StatusCode, raw
}

func replayed(t *testing.T, provider onesie.Provider, status int, body []byte) error {
	t.Helper()

	client, err := onesie.New(
		onesie.WithProvider(provider),
		onesie.WithAPIKey("replay"),
		onesie.WithBaseURL("https://replay.invalid"),
		onesie.WithEnv(func(string) (string, bool) { return "", false }),
		onesie.WithHTTPClient(&http.Client{Transport: replayTransport{status: status, body: body}}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = client.SystemOne(liveContext(t, time.Minute), onesie.Request{
		State:     "x",
		Questions: onesie.Questions{{ID: "q", Question: onesie.Noul{Instructions: "Is this a test?"}}},
	})

	return err
}

type replayTransport struct {
	status int
	body   []byte
}

func (r replayTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: r.status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(r.body)),
		Request:    req,
	}, nil
}

func liveCloudflareClient(t *testing.T) *onesie.Client {
	t.Helper()

	token, account := cloudflareCredentials(t)

	client, err := onesie.New(
		onesie.WithProvider(onesie.Cloudflare()),
		onesie.WithAPIKey(token),
		onesie.WithEnv(func(name string) (string, bool) {
			if name == "CLOUDFLARE_ACCOUNT_ID" {
				return account, true
			}

			return "", false
		}),
		onesie.WithUserAgent("onesie-integration"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return client
}

func liveContext(t *testing.T, d time.Duration) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), d)
	t.Cleanup(cancel)

	return ctx
}
