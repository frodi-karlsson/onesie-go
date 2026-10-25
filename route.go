package onesie

import "fmt"

type route struct {
	path   string
	takes  capabilities
	images func(body []byte) ([]byte, []RequestOption, error)
	encode func(body []byte) ([]byte, error)
	decode func(body []byte) ([]byte, error)
}

func (r route) refuse(provider string, needs capabilities) error {
	switch {
	// A Box question is an image question, so a route that takes no images takes no Box either.
	case (needs.images || needs.box) && !r.takes.images:
		return &ValidationError{Message: fmt.Sprintf(
			"the %s provider takes no images or box questions. The typesafe and openrouter providers do", provider)}
	case needs.box && !r.takes.box:
		return &ValidationError{Message: fmt.Sprintf("the %s provider takes no box questions", provider)}
	case needs.longContext && !r.takes.longContext:
		return &ValidationError{Message: fmt.Sprintf("the %s provider takes no long context", provider)}
	default:
		return nil
	}
}

type capabilities struct {
	images      bool
	box         bool
	longContext bool
}

func takesEverything() capabilities {
	return capabilities{images: true, box: true, longContext: true}
}

func oneRoute(r route) func(string) route {
	return func(string) route {
		return r
	}
}

func identity(body []byte) ([]byte, error) {
	return body, nil
}
