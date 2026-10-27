package onesie

func classifyBy(rules ...errorRule) func(int, any, string, Needs) error {
	return func(status int, body any, message string, needs Needs) error {
		for _, rule := range rules {
			if sentinel := rule(status, body, message, needs); sentinel != nil {
				return sentinel
			}
		}

		return nil
	}
}

type errorRule func(status int, body any, message string, needs Needs) error

func cloudflareAuth(_ int, body any, _ string, _ Needs) error {
	// Cloudflare refuses a token with 10000 on most endpoints, and with 9106 under a 400 on the
	// models search.
	if code := codeOf(body); code == "10000" || code == "9106" {
		return ErrAuthentication
	}

	return nil
}
