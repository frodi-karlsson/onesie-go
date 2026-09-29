# Maintaining

## Development

Run `make check` before pushing. It runs `golangci-lint` and the race enabled unit suite.
Run `make tidy` after changing imports or the Go version. Run `make test-integration` for the
live API suite.

## Repository settings

The scheduled integration workflow reads `TYPESAFE_API_KEY`, `OPENROUTER_API_KEY` and
`BERGET_API_KEY`. They must be configured as Actions secrets in this repository. The CLI
repository also uses them, but GitHub does not reveal their values for copying.

The CLI repository also has signing, notarization and Homebrew secrets. This library does not
publish binaries, so it does not need those secrets. It has no repository variables or environments.

Protect `main` against deletion and forced updates. Require signed commits and the `check`,
`test (macos-latest)` and `test (windows-latest)` CI checks. Protect release tags matching `v*`
against updates and deletion.
