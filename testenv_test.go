//go:build integration

package onesie_test

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func apiKey(t *testing.T) string {
	t.Helper()

	if key := strings.TrimSpace(os.Getenv("TYPESAFE_API_KEY")); key != "" {
		return key
	}

	if key := fromDotEnv(t, "TYPESAFE_API_KEY"); key != "" {
		return key
	}

	t.Skip("no TYPESAFE_API_KEY in the environment or .env, skipping the live API suite")

	return ""
}

func openRouterKey(t *testing.T) string {
	t.Helper()

	if key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")); key != "" {
		return key
	}

	if key := fromDotEnv(t, "OPENROUTER_API_KEY"); key != "" {
		return key
	}

	t.Skip("no OPENROUTER_API_KEY in the environment or .env, skipping the OpenRouter live cases")

	return ""
}

func levantoKey(t *testing.T) string {
	t.Helper()

	if key := strings.TrimSpace(os.Getenv("LEVANTO_API_KEY")); key != "" {
		return key
	}

	if key := fromDotEnv(t, "LEVANTO_API_KEY"); key != "" {
		return key
	}

	t.Skip("no LEVANTO_API_KEY in the environment or .env, skipping the Levanto live cases")

	return ""
}

func fromDotEnv(t *testing.T, name string) string {
	t.Helper()

	// Deliberately minimal: KEY=VALUE per line, with no quoting, interpolation or export keyword.

	file, err := os.Open(".env")
	if err != nil {
		return ""
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Logf("closing .env: %v", closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if found && strings.TrimSpace(key) == name {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

func cloudflareCredentials(t *testing.T) (string, string) {
	t.Helper()

	read := func(name string) string {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}

		return fromDotEnv(t, name)
	}

	token, account := read("CLOUDFLARE_API_TOKEN"), read("CLOUDFLARE_ACCOUNT_ID")
	if token == "" || account == "" {
		t.Skip("no CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID in the environment or .env, skipping the Cloudflare live cases")
	}

	return token, account
}
