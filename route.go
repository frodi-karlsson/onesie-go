package onesie

type route struct {
	path         string
	takes        capabilities
	images       func(members []member) ([]member, error)
	imagesHeader bool
	encode       func(body jsonBody) ([]byte, error)
	decode       func(body []byte) ([]byte, error)
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

	return newCapabilityError(provider, model, Needs{
		Images: missing.images, Box: missing.box, LongContext: missing.longContext,
	}, takers)
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
