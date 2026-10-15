GOVULNCHECK_VERSION := v1.8.0

.PHONY: check test test-integration lint fmt tidy vuln

check: ## Run lint and race enabled tests
	@$(MAKE) --no-print-directory lint
	@$(MAKE) --no-print-directory test

test: ## Run the unit tests with the race detector
	go test -race -timeout 2m ./...

test-integration: ## Run the live API suite when provider keys are configured
	go test -tags integration -race -count=1 -timeout 5m ./... -run 'TestLive|Integration' -v

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format the Go source
	golangci-lint fmt ./...

tidy: ## Tidy and verify the module
	go mod tidy
	go mod verify

vuln: ## Scan the module for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
