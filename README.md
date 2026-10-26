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

The client uses the TypeSafe provider by default and reads `TYPESAFE_API_KEY`. Pass
`onesie.WithAPIKey` for explicit credentials, `onesie.WithProvider` to select OpenRouter, Berget,
Levanto or Cloudflare, and `onesie.WithHTTPClient` to supply a transport. Requests accept a context so the calling
service can set its own deadline and cancellation policy.

`VerifyKey` lists the models and returns them. On TypeSafe, Levanto and Cloudflare the list itself
needs the key. Berget's list does not, so it also asks one small question with the default model,
which spends a few tokens. OpenRouter's list does not either, so it also requests the key endpoint,
which spends none. A key that endpoint refuses fails with `ErrAuthentication` or
`ErrPermissionDenied`, the same errors a refused listing returns.

Images and Box questions are experimental on TypeSafe's side. Put an `onesie.Image` from
`onesie.NewImage` in `Request.Assets` under a reference, and write the reference where the image
belongs, usually as a state value. `SystemOne` sends the images header itself.

A request is checked in two layers. `ValidateRequest` checks it for any provider.
`Provider.Encode` refuses what one provider does not take and returns the body it would send.
`SystemOne` runs both, in that order, before it sends. `Provider.Refuse` gives the same refusal
from a model and its `Needs`, before a request exists. The body `Encode` returns carries no
headers, so `SystemOne` also sends the images header for TypeSafe, and a blank model there is the
provider's built in default.

Set `Request.LongContext`, or pass `onesie.WithLongContext()` to one call, to raise the context
limits. A body from `MarshalBody` carries it as `long_context`, which `SystemOneRaw` turns back
into the header, since TypeSafe refuses the key itself.

Each provider gets images the way it reads them: TypeSafe as the assets map, OpenRouter as inline
image parts, Cloudflare's Jev as the assets map and Clef as an images array numbered in the order
the images are named, and Levanto and Berget refuse them, as `Provider.Refuse` reports.

`onesie.Cloudflare()` reads `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID`, or takes the account
inside `WithBaseURL`. Its default model is `typesafe/jev`, billed from AI Gateway credits, and
`clef`, `clef-flash` and `clef-omni` select Cloudflare's own models:

```go
client, err := onesie.New(onesie.WithProvider(onesie.Cloudflare()))
if err != nil {
	return err
}

result, err := client.SystemOne(ctx, onesie.Request{
	State:     "The checkout page has been down for an hour.",
	Model:     "clef-flash",
	Questions: onesie.Questions{{ID: "urgent", Question: onesie.Noul{Instructions: "Is this urgent?"}}},
})
```

Use the CLI for streams, question files, shell assertions, caching and calibration. Those workflows
remain in the onesie command.

## Development

Run `make check` for lint and race enabled tests. Run `make tidy` to verify the module files.
The live suite is available with `make test-integration`. It reads `TYPESAFE_API_KEY`,
`OPENROUTER_API_KEY`, `LEVANTO_API_KEY`, `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID` from the
environment or a local `.env` file.
