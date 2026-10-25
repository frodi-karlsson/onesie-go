package onesie

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ProviderNamed returns the provider a --provider value or ONESIE_PROVIDER names.
func ProviderNamed(name string) (Provider, error) {
	switch strings.TrimSpace(name) {
	case "", "typesafe":
		return TypeSafe(), nil
	case "openrouter":
		return OpenRouter(), nil
	case "berget":
		return Berget(), nil
	case "levanto":
		return Levanto(), nil
	default:
		return Provider{}, &ValidationError{
			Message: "unknown provider " + name + ". Use typesafe, openrouter, berget or levanto",
		}
	}
}

// TypeSafe is the TypeSafe System One API, the default provider.
func TypeSafe() Provider {
	return Provider{
		Name:            "typesafe",
		BaseURL:         DefaultBaseURL,
		EnvAPIKey:       EnvAPIKey,
		EnvBaseURL:      EnvBaseURL,
		EnvDefaultModel: EnvDefaultModel,
		defaultModel:    DefaultModel,
		requestIDHeader: "X-TypeSafe-Request-Id",
		modelsPath:      "/v1/models",
		decodeModels:    decodeTypeSafeModels,
		routeFor: oneRoute(route{
			path:   systemOnePath,
			takes:  takesEverything(),
			images: assetsMap,
			encode: identity,
			decode: identity,
		}),
	}
}

// OpenRouter serves the same model and wire protocol through OpenRouter.
func OpenRouter() Provider {
	return Provider{
		Name:            "openrouter",
		BaseURL:         "https://openrouter.ai/api",
		EnvAPIKey:       "OPENROUTER_API_KEY",
		defaultModel:    DefaultModel,
		requestIDHeader: "X-Generation-Id",
		modelsPath:      "/v1/models?output_modalities=decisions",
		decodeModels:    decodeOpenRouterModels,
		routeFor: oneRoute(route{
			path:   systemOnePath,
			takes:  takesEverything(),
			images: inlineParts,
			encode: identity,
			decode: identity,
		}),
	}
}

// Berget serves the same wire protocol with its own models, which do not include jev-latest.
func Berget() Provider {
	return Provider{
		Name:               "berget",
		BaseURL:            "https://api.berget.ai",
		EnvAPIKey:          "BERGET_API_KEY",
		defaultModel:       BergetDefaultModel,
		requestIDHeader:    "X-Request-Id",
		modelsPath:         "/v1/models/",
		decodeModels:       decodeBergetModels,
		modelsAnswerAnyKey: true,
		routeFor: oneRoute(route{
			path:   systemOnePath,
			takes:  capabilities{longContext: true},
			encode: identity,
			decode: identity,
		}),
	}
}

// Levanto serves the same wire protocol with its Sage models, which do not include jev-latest.
func Levanto() Provider {
	return Provider{
		Name:            "levanto",
		BaseURL:         "https://sage.levanto.ai",
		EnvAPIKey:       "LEVANTO_API_KEY",
		defaultModel:    LevantoDefaultModel,
		requestIDHeader: "X-Request-Id",
		modelsPath:      "/v1/models",
		decodeModels:    decodeTypeSafeModels,
		routeFor: oneRoute(route{
			path:   systemOnePath,
			takes:  capabilities{longContext: true},
			encode: identity,
			decode: identity,
		}),
	}
}

// Provider is a host that serves the System One API. Build one with TypeSafe, OpenRouter, Berget or
// Levanto.
// An empty env var name means the provider reads nothing from the environment for that setting.
type Provider struct {
	Name            string
	BaseURL         string
	EnvAPIKey       string
	EnvBaseURL      string
	EnvDefaultModel string

	defaultModel    string
	requestIDHeader string
	modelsPath      string
	decodeModels    func([]byte) ([]ModelCard, error)
	routeFor        func(model string) route

	modelsAnswerAnyKey bool
}

// ResolveModel reports the model a request will carry under this provider. It exists so a caller
// that prints a request without building a client fills the model the way a client would.
func (p Provider) ResolveModel(model string, lookupEnv func(string) (string, bool)) string {
	return orDefault(orEnv(model, lookupEnv, p.EnvDefaultModel), p.defaultModel)
}

func (p Provider) route(model string) route {
	if p.routeFor == nil {
		// A Provider built outside the package never reaches New, so it is only asked what it takes.
		return route{takes: takesEverything()}
	}

	return p.routeFor(model)
}

func decodeTypeSafeModels(body []byte) ([]ModelCard, error) {
	var wire struct {
		Models []ModelCard `json:"models"`
	}

	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}

	if wire.Models == nil {
		return nil, errors.New("expected a models list")
	}

	return wire.Models, nil
}

func decodeOpenRouterModels(body []byte) ([]ModelCard, error) {
	var wire struct {
		Data []struct {
			ID          string `json:"id"`
			Description string `json:"description"`
			Created     int64  `json:"created"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}

	if wire.Data == nil {
		return nil, errors.New("expected a models list")
	}

	cards := make([]ModelCard, 0, len(wire.Data))
	for _, model := range wire.Data {
		cards = append(cards, ModelCard{
			Name:        model.ID,
			Description: model.Description,
			ReleaseDate: time.Unix(model.Created, 0).UTC().Format(time.DateOnly),
		})
	}

	return cards, nil
}

func decodeBergetModels(body []byte) ([]ModelCard, error) {
	var wire struct {
		Data []struct {
			ID          string   `json:"id"`
			ModelType   string   `json:"model_type"`
			Aliases     []string `json:"aliases"`
			ReleaseDate string   `json:"release_date"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}

	if wire.Data == nil {
		return nil, errors.New("expected a models list")
	}

	// The list holds every model Berget serves, chat and speech included, and only the system-one
	// ones answer the System One endpoint.
	cards := []ModelCard{}

	for _, model := range wire.Data {
		if model.ModelType != "system-one" {
			continue
		}

		card := ModelCard{Name: model.ID, ReleaseDate: model.ReleaseDate}
		if len(model.Aliases) > 0 {
			card.Description = "aliases " + strings.Join(model.Aliases, ", ")
		}

		cards = append(cards, card)
	}

	return cards, nil
}
