package onesie

type route struct {
	path            string
	takes           capabilities
	images          func(members []member) ([]member, error)
	imagesHeader    bool
	encode          func(body jsonBody) ([]byte, error)
	decode          func(body []byte) ([]byte, error)
	requestIDHeader string
	describeError   func(body any) string
	classifyError   func(status int, body any, message string, needs Needs) error
}

func (r route) refuse(provider, model string, needs capabilities) error {
	missing := r.lacks(needs)
	if missing == (capabilities{}) {
		return nil
	}

	var takers []string

	for _, candidate := range Providers() {
		if candidate.name != provider && candidate.route(candidate.defaultModel).lacks(missing) == (capabilities{}) {
			takers = append(takers, candidate.name)
		}
	}

	return newCapabilityError(provider, model, missing.asNeeds(), takers)
}

func (r route) lacks(needs capabilities) capabilities {
	var missing capabilities

	// A Box question is an image question, so a route that takes no images takes no Box either.
	if !r.takes.images {
		missing.images = needs.images
		missing.box = needs.box
	}

	missing.box = missing.box || (needs.box && !r.takes.box)
	missing.longContext = needs.longContext && !r.takes.longContext

	return missing
}

type capabilities struct {
	images      bool
	box         bool
	longContext bool
}

func (c capabilities) asNeeds() Needs {
	return Needs{Images: c.images, Box: c.box, LongContext: c.longContext}
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

func (r route) reader() func(any) string {
	if r.describeError == nil {
		return readMessage
	}

	return r.describeError
}

func (r route) classify(status int, body any, message string, needs Needs) error {
	if r.classifyError == nil {
		return nil
	}

	return r.classifyError(status, body, message, needs)
}
