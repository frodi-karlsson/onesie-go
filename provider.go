package onesie

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	keyCheckByListing keyCheckMethod = iota
	keyCheckByQuestion
	keyCheckByEndpoint
)

const (
	typeSafeBaseURL = "https://api.typesafe.ai"
	jevLatest       = "jev-latest"
	jevPreview      = "jev-preview"
	bergetModel     = "systemone"
	levantoModel    = "sage-latest"
	cloudflareJev   = "typesafe/jev"
)

var versionedModel = regexp.MustCompile(`^.+-[0-9]+\.[0-9]+(\.[0-9]+)?$`)

// ProviderNamed returns the provider with this name, as Name reports it. An empty name is TypeSafe.
func ProviderNamed(name string) (Provider, error) {
	wanted := strings.TrimSpace(name)
	if wanted == "" {
		return TypeSafe(), nil
	}

	for _, provider := range Providers() {
		if provider.name == wanted {
			return provider, nil
		}
	}

	return Provider{}, &ValidationError{Message: "unknown provider '" + printable(name) + "'. Use " + joinNames(providerNames(), "or")}
}

func providerNames() []string {
	providers := Providers()
	names := make([]string, 0, len(providers))

	for _, provider := range providers {
		names = append(names, provider.name)
	}

	return names
}

// Providers lists every provider this package serves, TypeSafe first, in the order a refusal names them.
func Providers() []Provider {
	return []Provider{TypeSafe(), OpenRouter(), Berget(), Levanto(), Cloudflare()}
}

func joinNames(names []string, conjunction string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}

	return strings.Join(names[:len(names)-1], ", ") + " " + conjunction + " " + names[len(names)-1]
}

// TypeSafe is the TypeSafe System One API, the default provider.
func TypeSafe() Provider {
	return Provider{
		name:            "typesafe",
		defaultBaseURL:  typeSafeBaseURL,
		envAPIKey:       "TYPESAFE_API_KEY",
		envBaseURL:      "TYPESAFE_BASE_URL",
		envDefaultModel: "TYPESAFE_DEFAULT_MODEL",
		defaultModel:    jevLatest,
		boxModel:        jevPreview,
		requestIDHeader: "X-TypeSafe-Request-Id",
		modelsPath:      "/v1/models",
		decodeModels:    decodeTypeSafeModels,
		pinsVersions:    true,
		routeFor: oneRoute(route{
			path:         systemOnePath,
			takes:        takesEverything(),
			imagesHeader: true,
			encode:       encodeAsIs,
			decode:       identity,
			classifyError: classifyBy(
				unknownModel("Unknown model"), contextTooLong, boxNotServed, countDetail, questionLimit,
			),
		}),
	}
}

// OpenRouter serves the same model and wire protocol through OpenRouter.
func OpenRouter() Provider {
	return Provider{
		name:            "openrouter",
		defaultBaseURL:  "https://openrouter.ai/api",
		envAPIKey:       "OPENROUTER_API_KEY",
		defaultModel:    jevLatest,
		boxModel:        jevPreview,
		requestIDHeader: "X-Generation-Id",
		modelsPath:      "/v1/models?output_modalities=decisions",
		decodeModels:    decodeOpenRouterModels,
		keyCheck:        keyCheck{by: keyCheckByEndpoint, path: "/v1/key"},
		routeFor: oneRoute(route{
			path:          systemOnePath,
			takes:         takesEverything(),
			images:        inlineParts,
			encode:        encodeAsIs,
			decode:        identity,
			describeError: readOpenRouterMessage,
			forwarded:     openRouterForwarded,
			classifyError: classifyBy(
				unknownModel("Unknown model"), unknownModel("does not exist"), contextTooLong, boxNotServed,
				countDetail, questionLimit,
			),
		}),
	}
}

func readOpenRouterMessage(body any) string {
	if text, found := openRouterMessage(body); found {
		return firstIssue(text)
	}

	return readMessage(body)
}

func openRouterForwarded(body any) any {
	if text, found := openRouterMessage(body); found {
		if forwarded, ok := forwardedText(text); ok {
			return decodeBody([]byte(forwarded))
		}
	}

	return nil
}

func openRouterMessage(body any) (string, bool) {
	fields, isObject := body.(map[string]any)
	if !isObject {
		return "", false
	}

	nested, isNested := fields["error"].(map[string]any)
	if !isNested {
		return "", false
	}

	text, isText := nested["message"].(string)

	return text, isText
}

func forwardedText(text string) (string, bool) {
	// OpenRouter forwards a TypeSafe error as the text HTTP 400: followed by TypeSafe's body.
	rest, isForwarded := strings.CutPrefix(text, "HTTP ")
	code, body, hasColon := strings.Cut(rest, ":")

	if status, err := strconv.Atoi(code); !isForwarded || !hasColon || err != nil || status < 100 || status > 599 {
		return "", false
	}

	return strings.TrimSpace(body), true
}

func firstIssue(text string) string {
	// OpenRouter's validation puts its issue list in the message as a JSON string.
	var issues []struct {
		Path    any    `json:"path"`
		Message string `json:"message"`
	}

	if json.Unmarshal([]byte(text), &issues) != nil || len(issues) == 0 {
		return text
	}

	path := validationPath(issues[0].Path)
	if path == "" {
		return issues[0].Message
	}

	return path + ": " + issues[0].Message
}

// Berget serves the same wire protocol with its own models, which do not include jev-latest.
func Berget() Provider {
	return Provider{
		name:            "berget",
		defaultBaseURL:  "https://api.berget.ai",
		envAPIKey:       "BERGET_API_KEY",
		defaultModel:    bergetModel,
		requestIDHeader: "X-Request-Id",
		modelsPath:      "/v1/models/",
		decodeModels:    decodeBergetModels,
		keyCheck:        keyCheck{by: keyCheckByQuestion},
		routeFor: oneRoute(route{
			path:          systemOnePath,
			takes:         capabilities{longContext: true},
			encode:        encodeAsIs,
			decode:        identity,
			classifyError: classifyBy(unknownModel("Model not found")),
		}),
	}
}

// Levanto serves the same wire protocol with its Sage models, which do not include jev-latest.
func Levanto() Provider {
	return Provider{
		name:            "levanto",
		defaultBaseURL:  "https://sage.levanto.ai",
		envAPIKey:       "LEVANTO_API_KEY",
		defaultModel:    levantoModel,
		requestIDHeader: "X-Request-Id",
		modelsPath:      "/v1/models",
		decodeModels:    decodeTypeSafeModels,
		routeFor: oneRoute(route{
			path:          systemOnePath,
			takes:         capabilities{longContext: true},
			encode:        encodeAsIs,
			decode:        identity,
			classifyError: classifyBy(unknownModel("unknown model")),
		}),
	}
}

// Cloudflare serves TypeSafe's Jev through AI Gateway and its own Clef models on Workers AI. Its
// base URL holds an account id.
func Cloudflare() Provider {
	return Provider{
		name:            "cloudflare",
		envAPIKey:       "CLOUDFLARE_API_TOKEN",
		envAccountID:    "CLOUDFLARE_ACCOUNT_ID",
		defaultModel:    cloudflareJev,
		requestIDHeader: "Cf-Ray",
		modelsPath:      "/models/search?search=clef",
		decodeModels:    decodeCloudflareModels,
		accountBaseURL:  cloudflareBaseURL,
		routeFor:        cloudflareRoute,
	}
}

// Provider is a host that serves the System One API. Take one from ProviderNamed, Providers or a
// constructor such as TypeSafe, since the zero value is refused wherever a provider is used.
type Provider struct {
	name            string
	defaultBaseURL  string
	envAPIKey       string
	envBaseURL      string
	envDefaultModel string
	envAccountID    string
	defaultModel    string
	boxModel        string
	requestIDHeader string
	modelsPath      string
	decodeModels    func([]byte) ([]ModelCard, error)
	routeFor        func(model string) route
	keyCheck        keyCheck
	accountBaseURL  func(account string) string
	pinsVersions    bool
}

// Name is the provider's name, as ProviderNamed takes it.
func (p Provider) Name() string {
	return p.name
}

// EnvAPIKey names the environment variable New reads the API key from.
func (p Provider) EnvAPIKey() string {
	return p.envAPIKey
}

// EnvBaseURL names the environment variable that overrides the base URL, or is empty when none does.
func (p Provider) EnvBaseURL() string {
	return p.envBaseURL
}

// EnvDefaultModel names the environment variable that overrides the default model, or is empty
// when none does.
func (p Provider) EnvDefaultModel() string {
	return p.envDefaultModel
}

// EnvAccountID names the environment variable holding the account id, or is empty for a provider
// without accounts.
func (p Provider) EnvAccountID() string {
	return p.envAccountID
}

// DefaultModel is the model a request carries when it names none and nothing overrides it.
func (p Provider) DefaultModel() string {
	return p.defaultModel
}

// BoxModel names a model known to answer a box question, or is empty when none is known. Refuse
// says whether a model takes a box at all.
func (p Provider) BoxModel() string {
	return p.boxModel
}

// ResponseBody encodes result as the route for model returns it, a blank model being the default. It
// leaves out the request id, which travels in a response header, so a fake that wants one sets that header.
func (p Provider) ResponseBody(model string, result *Result) ([]byte, error) {
	if err := p.check(); err != nil {
		return nil, err
	}

	if result == nil {
		return nil, &ValidationError{Message: "the result is nil"}
	}

	inBody := *result
	inBody.RequestID = ""

	body, err := json.Marshal(&inBody)
	if err != nil {
		return nil, err
	}

	if via := p.route(modelOr(model, p.defaultModel)); via.wrapResult != nil {
		return via.wrapResult(body)
	}

	return body, nil
}

// DefaultBaseURL is the base URL used when nothing overrides it, or is empty when the base URL
// depends on an account.
func (p Provider) DefaultBaseURL() string {
	return p.defaultBaseURL
}

// Pinned reports whether model names one fixed version whose answers never change. Only a TypeSafe
// name ending in a two or three part version, such as jev-1.13.0, is pinned.
func (p Provider) Pinned(model string) bool {
	return p.pinsVersions && versionedModel.MatchString(model)
}

func (p Provider) check() error {
	if p.routeFor != nil {
		return nil
	}

	return &ValidationError{Message: "the provider is empty. Use ProviderNamed with " + joinNames(providerNames(), "or")}
}

type keyCheck struct {
	by   keyCheckMethod
	path string
}

type keyCheckMethod int

// ResolveModel reports the model a request carries under this provider, so a caller that prints a
// request without a client fills the model as a client would.
func (p Provider) ResolveModel(model string, lookupEnv func(string) (string, bool)) string {
	return modelOr(orEnv(model, lookupEnv, p.envDefaultModel), p.defaultModel)
}

func modelOr(model, fallback string) string {
	if named := strings.TrimSpace(model); named != "" {
		return named
	}

	return fallback
}

func (p Provider) route(model string) route {
	via := p.routeFor(model)
	via.requestIDHeader = p.requestIDHeader

	return via
}

// ResolveBaseURL resolves the base URL as New does: explicit, then accountID, then EnvBaseURL, then
// EnvAccountID, then DefaultBaseURL. It checks the URL's shape and needs no key.
func (p Provider) ResolveBaseURL(
	explicit, accountID string,
	lookupEnv func(string) (string, bool),
) (string, BaseURLSource, error) {
	if err := p.check(); err != nil {
		return "", BaseURLSourceUnknown, err
	}

	raw, source, err := p.chooseBaseURL(strings.TrimSpace(explicit), strings.TrimSpace(accountID), lookupEnv)
	if err != nil {
		return "", source, err
	}

	if refused := checkBaseURL(raw, source); refused != nil {
		return "", source, refused
	}

	return strings.TrimRight(raw, "/"), source, nil
}

func (p Provider) chooseBaseURL(
	explicit, accountID string,
	lookupEnv func(string) (string, bool),
) (string, BaseURLSource, error) {
	if accountID != "" {
		if err := p.checkAccountID(accountID, "the account id"); err != nil {
			return "", BaseURLFromAccount, err
		}
	}

	switch fromEnv := orEnv("", lookupEnv, p.envBaseURL); {
	case explicit != "":
		return explicit, BaseURLFromOption, nil
	case accountID != "":
		return p.accountBaseURL(accountID), BaseURLFromAccount, nil
	case fromEnv != "":
		return fromEnv, BaseURLFromEnv, nil
	case p.envAccountID == "":
		return p.defaultBaseURL, BaseURLFromDefault, nil
	}

	account := orEnv("", lookupEnv, p.envAccountID)
	if account == "" {
		return "", BaseURLSourceUnknown, &ValidationError{Message: fmt.Sprintf(
			"no %s account id. Set %s, pass WithAccountID, or pass the base URL %s",
			p.name, p.envAccountID, p.accountBaseURL("<account_id>")), kind: ErrNoAccountID}
	}

	if err := p.checkAccountID(account, p.envAccountID); err != nil {
		return "", BaseURLFromAccount, err
	}

	return p.accountBaseURL(account), BaseURLFromAccount, nil
}

func (p Provider) checkAccountID(account, subject string) error {
	if p.accountBaseURL == nil {
		return &ValidationError{Message: "the " + p.name + " provider takes no account id"}
	}

	if decoded, err := hex.DecodeString(account); err != nil || len(decoded) != 16 {
		return &ValidationError{Message: subject + " must be 32 hexadecimal characters"}
	}

	return nil
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

	for i := range wire.Models {
		wire.Models[i].ReleaseDate = dayOf(wire.Models[i].ReleaseDate)
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
			ReleaseDate: unixDay(model.Created),
		})
	}

	return cards, nil
}

func unixDay(seconds int64) string {
	if seconds <= 0 {
		return ""
	}

	return time.Unix(seconds, 0).UTC().Format(time.DateOnly)
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

	// The list holds every model Berget serves, chat and speech included, and only a model whose
	// model_type is system-one answers the System One endpoint.
	cards := []ModelCard{}

	for _, model := range wire.Data {
		if model.ModelType != "system-one" {
			continue
		}

		card := ModelCard{Name: model.ID, ReleaseDate: dayOf(model.ReleaseDate)}
		if len(model.Aliases) > 0 {
			card.Description = "aliases " + strings.Join(model.Aliases, ", ")
		}

		cards = append(cards, card)
	}

	return cards, nil
}

func dayOf(date string) string {
	day, _, _ := strings.Cut(strings.TrimSpace(date), "T")
	day, _, _ = strings.Cut(day, " ")

	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return ""
	}

	return day
}
