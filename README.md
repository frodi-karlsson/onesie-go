# Onesie Go client

The `onesie` package is a Go client for the TypeSafe System One API and compatible providers.
The [onesie CLI](https://github.com/frodi-karlsson/onesie) uses this client.

```go
import "github.com/frodi-karlsson/onesie-go"

client, err := onesie.New(onesie.WithAPIKey(apiKey))
if err != nil {
	return err
}

result, err := client.SystemOne(ctx, onesie.Request{
	State: "Please restore service today.",
	Questions: onesie.Questions{
		{ID: "urgent", Question: onesie.Noul{Instructions: "Is this urgent?"}},
		{ID: "team", Question: onesie.Choice{
			Instructions: "Which team should handle this?",
			Criteria: onesie.Criteria{
				{Name: "billing", Desc: "Payments, invoicing, refunds"},
				{Name: "technical", Desc: "Bugs, outages, integrations"},
				{Name: "other"},
			},
		}},
	},
})
if err != nil {
	return err
}

urgent, err := result.Noul("urgent")
if err != nil {
	return err
}

fmt.Println(urgent.Noul)
```

## Providers

The client talks to TypeSafe by default. Pass `onesie.WithProvider` with `onesie.ProviderNamed` or
a constructor such as `onesie.OpenRouter()` to use OpenRouter, Berget, Levanto or Cloudflare.
Each provider reads its key from the variable `Provider.EnvAPIKey` names, unless
`onesie.WithAPIKey` or `onesie.WithAPIKeyFunc` supplies it. Cloudflare also needs an account, from
`onesie.WithAccountID` or the variable `Provider.EnvAccountID` names.

## Errors

A request the client refuses before sending matches `onesie.ErrValidation`.
A response outside 2xx is an `*onesie.APIError`, matched with `errors.Is` against a sentinel such as `onesie.ErrUnknownModel`.
`SystemOne` returns a `ResponseError` matching `ErrResponse` when an answer does not fit its question.
Every error type the package defines has `Reason()`, its message without the `onesie: ` prefix.
An error from the function passed to `onesie.WithAPIKeyFunc` is returned as it is, without one.

## Reference

The full API is documented at [pkg.go.dev](https://pkg.go.dev/github.com/frodi-karlsson/onesie-go).

## Development

Run `make check` for lint and race enabled tests. Run `make tidy` to verify the module files.
The live suite is available with `make test-integration`. It reads `TYPESAFE_API_KEY`,
`OPENROUTER_API_KEY`, `BERGET_API_KEY`, `LEVANTO_API_KEY`, `CLOUDFLARE_API_TOKEN` and
`CLOUDFLARE_ACCOUNT_ID` from the environment or a local `.env` file, and skips the cases whose keys
are missing.
