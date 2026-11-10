# Maintaining

## Development

Run `make check` before pushing. It runs `golangci-lint` and the race enabled unit suite.
Run `make vuln` to scan the module with `govulncheck`, which CI also runs.
Run `make tidy` after changing imports or the Go version.

`testdata/encode` holds the body `MarshalBody` writes and the payload each route sends for every case
of the encode corpus in `corpus_test.go`, or the error a route refuses it with. After a change to the
encoding made on purpose, rewrite them with `go test -run 'TestMarshalBody|TestClientSystemOne' -update-encode`
and review the diff, which shows exactly what a provider now receives.

Run `make test-integration` for the live API suite. It reads `TYPESAFE_API_KEY`,
`OPENROUTER_API_KEY`, `BERGET_API_KEY`, `LEVANTO_API_KEY`, `CLOUDFLARE_API_TOKEN` and
`CLOUDFLARE_ACCOUNT_ID` from the environment or a local `.env` file, and skips the cases whose keys
are missing.

## Repository settings

The scheduled integration workflow reads `TYPESAFE_API_KEY`, `OPENROUTER_API_KEY`,
`LEVANTO_API_KEY`, `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID`. They must be configured as
Actions secrets in this repository. The Cloudflare Jev cases spend AI Gateway credits on every run,
and fail with 402 when the credits run out.

The repository has no variables or environments.

Protect `main` against deletion and forced updates. Require signed commits and the `check`,
`test (macos-latest)` and `test (windows-latest)` CI checks. Protect release tags matching `v*`
against updates and deletion.
