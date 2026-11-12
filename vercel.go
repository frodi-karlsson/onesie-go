package onesie

import (
	"cmp"
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// A model is listed when the gateway catalogue says it reads text only and a live request with an
// image was refused. The gateway forwards such a request anyway, and each vendor fails it differently.
var textOnlyVercelModels = []string{
	"baseten/elephant-1",
	"baseten/joey-1",
	"baseten/mammoth-1",
	"convaiinnovations/laya",
	"microsoft/microsoft-decision-1",
	"nace-ai/drex-v2.0",
	"parallel/parallel-decisions-fast",
	"scaledown/scaledown-classify-1",
}

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
		routeFor:        vercelRoute,
	}
}

func vercelRoute(model string) route {
	takes := takesEverything()
	takes.images = !slices.Contains(textOnlyVercelModels, model)

	return route{
		path:          systemOnePath,
		takes:         takes,
		imagesHeader:  true,
		encode:        encodeAsIs,
		decode:        withGatewayCost,
		wrapResult:    gatewayResult,
		describeError: readVercelMessage,
		forwarded:     vercelForwarded,
		classifyError: classifyBy(
			errorType("model_not_found", ErrUnknownModel),
			errorType("AI_DecisionUnsupportedQuestionTypeError", ErrNotServed),
			contextTooLong, boxNotServed, countDetail, arrayLimit, questionLimit,
		),
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

func gatewayResult(result []byte) ([]byte, error) {
	var fields map[string]json.RawMessage

	var wire struct {
		Usage map[string]json.RawMessage `json:"usage"`
	}

	if err := json.Unmarshal(result, &fields); err != nil {
		return nil, err
	}

	if err := json.Unmarshal(result, &wire); err != nil {
		return nil, err
	}

	cost, costed := numberIn(wire.Usage["cost"])
	if !costed {
		// A result without a cost reads the same on the gateway as on TypeSafe.
		return result, nil
	}

	delete(wire.Usage, "cost")

	usage, err := json.Marshal(wire.Usage)
	if err != nil {
		return nil, err
	}

	metadata, err := json.Marshal(map[string]any{"gateway": map[string]string{"cost": strconv.FormatFloat(cost, 'f', -1, 64)}})
	if err != nil {
		return nil, err
	}

	fields["usage"], fields["provider_metadata"] = usage, metadata

	return json.Marshal(fields)
}

func numberIn(raw json.RawMessage) (float64, bool) {
	var number float64
	if raw == nil || json.Unmarshal(raw, &number) != nil {
		return 0, false
	}

	return number, true
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
	return cmp.Or(refusedAttempt(body), readMessage(body))
}

func vercelForwarded(body any) any {
	// The gateway forwards some TypeSafe errors as a JSON object, such as
	// {"error_type":"max_tokens_exceeded"} for a state past the context limit.
	text := readVercelMessage(body)
	if !strings.HasPrefix(text, "{") {
		return nil
	}

	return decodeBody([]byte(text))
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
			if aboutTheBody(attempt.StatusCode) && attempt.Error != "" {
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

func aboutTheBody(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}
