package onesie

import (
	"cmp"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// Vercel serves TypeSafe's Jev and the decision models of other vendors, such as
// google/gemini-decision-1, through Vercel AI Gateway on the System One wire protocol.
func Vercel() Provider {
	return Provider{
		name:            "vercel",
		defaultBaseURL:  "https://ai-gateway.vercel.sh/typesafe",
		envAPIKey:       "AI_GATEWAY_API_KEY",
		defaultModel:    jevLatest,
		boxModel:        jevLatest,
		requestIDHeader: "X-Vercel-Id",
		modelsPath:      "/v1/models",
		decodeModels:    decodeTypeSafeModels,
		routeFor: oneRoute(route{
			path:          systemOnePath,
			takes:         takesEverything(),
			imagesHeader:  true,
			encode:        encodeAsIs,
			decode:        withGatewayCost,
			describeError: readVercelMessage,
			classifyError: classifyBy(
				errorType("model_not_found", ErrUnknownModel),
				errorType("AI_DecisionUnsupportedQuestionTypeError", ErrNotServed),
				contextTooLong, boxNotServed, countDetail, arrayLimit, questionLimit,
			),
		}),
	}
}

func withGatewayCost(body []byte) ([]byte, error) {
	// A body that does not fit is left whole, so the result decoding names what is wrong with it.
	if lifted, found := liftedCost(body); found {
		return lifted, nil
	}

	return body, nil
}

func liftedCost(body []byte) ([]byte, bool) {
	var fields map[string]json.RawMessage

	var wire struct {
		Usage    map[string]json.RawMessage `json:"usage"`
		Metadata struct {
			Gateway struct {
				Cost json.RawMessage `json:"cost"`
			} `json:"gateway"`
		} `json:"provider_metadata"`
	}

	if json.Unmarshal(body, &fields) != nil || json.Unmarshal(body, &wire) != nil ||
		wire.Usage == nil || wire.Usage["cost"] != nil {
		return nil, false
	}

	cost, found := gatewayCost(wire.Metadata.Gateway.Cost)
	if !found {
		return nil, false
	}

	var err error
	if wire.Usage["cost"], err = json.Marshal(cost); err != nil {
		return nil, false
	}

	if fields["usage"], err = json.Marshal(wire.Usage); err != nil {
		return nil, false
	}

	lifted, err := json.Marshal(fields)

	return lifted, err == nil
}

func gatewayCost(raw json.RawMessage) (float64, bool) {
	// The gateway sends the cost in USD as a decimal string, such as "0.00001155".
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return 0, false
	}

	cost, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || math.IsInf(cost, 0) || math.IsNaN(cost) || cost < 0 {
		return 0, false
	}

	return cost, true
}

func readVercelMessage(body any) string {
	// A fallback provider's error replaces the model's own in the message, so the first attempt
	// the request itself failed says more.
	text := cmp.Or(refusedAttempt(body), readMessage(body))

	// The gateway forwards some TypeSafe errors as a JSON object, such as
	// {"error_type":"max_tokens_exceeded"} for a state past the context limit.
	var forwarded struct {
		Message   string `json:"message"`
		ErrorType string `json:"error_type"`
	}

	if !strings.HasPrefix(text, "{") || json.Unmarshal([]byte(text), &forwarded) != nil {
		return text
	}

	return cmp.Or(forwarded.Message, forwarded.ErrorType, text)
}

func refusedAttempt(body any) string {
	encoded, err := json.Marshal(body)
	if err != nil {
		return ""
	}

	var wire struct {
		Metadata struct {
			Gateway struct {
				Routing struct {
					ModelAttempts []struct {
						ProviderAttempts []struct {
							Error      string `json:"error"`
							StatusCode int    `json:"statusCode"`
						} `json:"providerAttempts"`
					} `json:"modelAttempts"`
				} `json:"routing"`
			} `json:"gateway"`
		} `json:"providerMetadata"`
	}

	if json.Unmarshal(encoded, &wire) != nil {
		return ""
	}

	for _, model := range wire.Metadata.Gateway.Routing.ModelAttempts {
		for _, attempt := range model.ProviderAttempts {
			if attempt.StatusCode >= 400 && attempt.StatusCode < 500 && attempt.Error != "" {
				return attempt.Error
			}
		}
	}

	return ""
}

func errorType(kind string, sentinel error) errorRule {
	return func(r reply) error {
		fields, isObject := r.body.(map[string]any)
		if !isObject {
			return nil
		}

		if nested, isNested := fields["error"].(map[string]any); isNested && nested["type"] == kind {
			return sentinel
		}

		return nil
	}
}

func arrayLimit(r reply) error {
	if r.status != http.StatusBadRequest {
		return nil
	}

	// The gateway checks the body before TypeSafe does, and words a count past a bound as
	// questions.rate.criteria: Too big: expected array to have <=10 items.
	if strings.Contains(r.message, " questions.") && strings.Contains(r.message, "expected array to have") {
		return ErrQuestionLimit
	}

	return nil
}
