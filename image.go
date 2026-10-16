package onesie

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
)

// The image limits the API documents. ValidateRequest checks every one a file's header can show.
const (
	// MaxImageBytes is the largest image file, before base64.
	MaxImageBytes = 16 << 20
	// MinImageSide is the narrowest an image's width or height may be, in pixels.
	MinImageSide = 280
	// MaxImageSide is the widest an image's width or height may be, in pixels.
	MaxImageSide = 1568
	// MinImagePixels is the fewest pixels an image may have, a 280 by 280 square. Any image whose
	// sides fit is within both pixel limits, so only the sides are checked.
	MinImagePixels = MinImageSide * MinImageSide
	// MaxImagePixels is the most pixels an image may have, a 1568 by 1568 square.
	MaxImagePixels = MaxImageSide * MaxImageSide
	// MaxBodyBytes is the largest request body, base64 included.
	MaxBodyBytes = 32 << 20
)

var (
	pngMagic  = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	jpegMagic = []byte{0xff, 0xd8, 0xff}
)

// NewImage reads the format from the file's header and refuses anything but PNG or JPEG.
func NewImage(data []byte) (Image, error) {
	format, err := formatOf(data)
	if err != nil {
		return Image{}, err
	}

	if _, err := configOf(format, data); err != nil {
		return Image{}, err
	}

	return Image{Format: format, Data: data}, nil
}

// Assets maps a reference string to the image it stands for. The API puts the image wherever the
// reference appears in a string value of the state, the instructions or the criteria.
type Assets map[string]Image

// Image is one PNG or JPEG file. Build one with NewImage to have its format read from the file.
type Image struct {
	Format string
	Data   []byte
}

// MarshalJSON writes the asset shape the API reads, with the bytes as standard padded base64.
func (i Image) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type   string `json:"type"`
		Format string `json:"format"`
		Data   string `json:"data_base64"`
	}{Type: "image", Format: wireFormat(i.Format), Data: base64.StdEncoding.EncodeToString(i.Data)})
}

func formatOf(data []byte) (string, error) {
	switch {
	case bytes.HasPrefix(data, pngMagic):
		return "png", nil
	case bytes.HasPrefix(data, jpegMagic):
		return "jpg", nil
	default:
		return "", &ValidationError{Message: "an image must be PNG or JPEG. Convert it to one of them first"}
	}
}

func configOf(format string, data []byte) (image.Config, error) {
	// The decoders are called directly rather than through image.DecodeConfig, which would need
	// them registered globally for every program that imports this package.
	decode := png.DecodeConfig
	name := "PNG"

	if format == "jpg" {
		decode = jpeg.DecodeConfig
		name = "JPEG"
	}

	config, err := decode(bytes.NewReader(data))
	if err != nil {
		return image.Config{}, &ValidationError{Message: "the " + name + " header cannot be read: " + err.Error()}
	}

	return config, nil
}

func wireFormat(format string) string {
	lower := strings.ToLower(format)
	if lower == "jpeg" {
		return "jpg"
	}

	return lower
}
