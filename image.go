package onesie

import (
	"bytes"
	"encoding/base64"
	"encoding/json/jsontext"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
)

// The image limits the API documents. NewImage checks every one a file's header can show.
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

// NewImage reads the format from the file's header and refuses an image the API would refuse: one
// that is not PNG or JPEG, a file over MaxImageBytes, or a side outside MinImageSide to MaxImageSide.
func NewImage(data []byte) (Image, error) {
	format, err := formatOf(data)
	if err != nil {
		return Image{}, err
	}

	if len(data) > MaxImageBytes {
		return Image{}, &ValidationError{Message: fmt.Sprintf(
			"the image is %d bytes, and an image is at most %d. Resize it to at most %d pixels a side "+
				"or save it as a smaller JPEG", len(data), MaxImageBytes, MaxImageSide)}
	}

	config, err := configOf(format, data)
	if err != nil {
		return Image{}, err
	}

	if !sideFits(config.Width) || !sideFits(config.Height) {
		return Image{}, &ValidationError{Message: fmt.Sprintf(
			"the image is %d by %d pixels, and each side must be %d to %d. Resize it first",
			config.Width, config.Height, MinImageSide, MaxImageSide)}
	}

	return encodedImage(format, data), nil
}

// Assets maps a reference string to the image it stands for. The API puts the image wherever the
// reference appears in a string value of the state, the instructions or the criteria.
type Assets map[string]Image

// Image is one PNG or JPEG file the API takes, built by NewImage. A request with a zero Image is refused.
type Image struct {
	format string
	// asset is the JSON object the API reads, encoded once. The encode is the image's only copy of the file.
	asset []byte
	// data is where the base64 sits in asset, so a data URL reads it without a copy.
	data struct{ start, end int }
}

// Format is the image's format, png or jpg, as read from its header.
func (i Image) Format() string {
	return i.format
}

// MarshalJSON writes the asset shape the API reads, with the bytes as standard padded base64. It
// returns a copy, so a caller cannot change the image.
func (i Image) MarshalJSON() ([]byte, error) {
	return bytes.Clone(i.encoded()), nil
}

// MarshalJSONTo writes what MarshalJSON returns to enc, without a copy.
func (i Image) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteValue(i.encoded())
}

func (i Image) encoded() []byte {
	if i.asset == nil {
		return encodedImage(i.format, nil).asset
	}

	return i.asset
}

// base64 is the image's data as standard padded base64, a slice of its encoded asset.
func (i Image) base64() []byte {
	return i.asset[i.data.start:i.data.end]
}

func encodedImage(format string, data []byte) Image {
	// Every byte written here is plain ASCII that JSON needs no escape for: the format is png, jpg or
	// empty, and base64 uses letters, digits, plus, slash and equals.
	const head, middle, tail = `{"type":"image","format":"`, `","data_base64":"`, `"}`

	size := base64.StdEncoding.EncodedLen(len(data))
	asset := make([]byte, 0, len(head)+len(format)+len(middle)+size+len(tail))
	asset = append(asset, head...)
	asset = append(asset, format...)
	asset = append(asset, middle...)
	start := len(asset)
	asset = base64.StdEncoding.AppendEncode(asset, data)
	end := len(asset)
	asset = append(asset, tail...)

	img := Image{format: format, asset: asset}
	img.data.start, img.data.end = start, end

	return img
}

func (i Image) built() bool {
	return i.format != ""
}

func sideFits(side int) bool {
	return side >= MinImageSide && side <= MaxImageSide
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
		return image.Config{}, &ValidationError{Message: "the " + name + " header cannot be read: " + err.Error(), cause: err}
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
