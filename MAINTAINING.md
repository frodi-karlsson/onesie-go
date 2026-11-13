# Maintaining

## Development

Run `make check` before pushing. It runs `golangci-lint` and the race enabled unit suite.
Run `make vuln` to scan the module with `govulncheck`, which CI also runs.
Run `make tidy` after changing imports or the Go version.

`testdata/encode` holds the body `MarshalBody` writes and the payload each route sends for every case
of the encode corpus in `corpus_test.go`, or the error a route refuses it with. After a change to the
encoding made on purpose, rewrite them with `go test -run 'TestMarshalBody|TestClientSystemOne' -update-encode`
and review the diff, which shows exactly what a provider receives.

Run `make test-integration` for the live API suite. It reads `TYPESAFE_API_KEY`,
`OPENROUTER_API_KEY`, `BERGET_API_KEY`, `LEVANTO_API_KEY`, `CLOUDFLARE_API_TOKEN`,
`CLOUDFLARE_ACCOUNT_ID` and `AI_GATEWAY_API_KEY` from the environment or a local `.env` file, and skips the cases whose keys
are missing.

The suite also reads the Vercel AI Gateway catalogue, which needs no key, and fails when the decision
models it lists as text only differ from `textOnlyVercelModels` in `vercel.go`. Send a new model a
request with an image before adding it, since the catalogue says text only for some models that read
images. Those go in the exception list beside the check, with the reason.

## Repository settings

The scheduled integration workflow reads `TYPESAFE_API_KEY`, `OPENROUTER_API_KEY`,
`LEVANTO_API_KEY`, `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID` and `AI_GATEWAY_API_KEY`. They
must be configured as Actions secrets in this repository. The Cloudflare Jev cases spend AI Gateway
credits on every run, and fail with 402 when the credits run out. The Vercel cases spend Vercel AI
Gateway credits, and fail with 403 when the team has no paid credits.

The repository has no variables or environments.

Protect `main` against deletion and forced updates. Require signed commits and the `check`,
`test (macos-latest)` and `test (windows-latest)` CI checks. Protect release tags matching `v*`
against updates and deletion.
