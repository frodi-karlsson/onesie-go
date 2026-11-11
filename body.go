package onesie

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"io"
)

var (
	errNotObject   = errors.New("the body is not an object")
	errAfterObject = errors.New("the body has data after its object")
)

type jsonBody struct {
	members []member
	raw     []byte
	changed bool
	// images are the Images a typed body was written from, so a route reads them, not the bytes. Only the
	// body the writer returns has them, so a body with changed members is read from those members.
	images Assets
}

func (b jsonBody) with(members []member) jsonBody {
	return jsonBody{members: members, raw: b.raw, changed: true}
}

func (b jsonBody) dataURLs() (map[string]string, error) {
	if b.images != nil {
		return dataURLsFrom(b.images), nil
	}

	// A raw body has only its bytes, so its assets are read and checked here.
	return dataURLsOf(b.members)
}

func encodeAsIs(b jsonBody) ([]byte, error) {
	return b.bytes()
}

func (b jsonBody) bytes() ([]byte, error) {
	if !b.changed {
		return b.raw, nil
	}

	return writeMembers(b.members)
}

func readMembers(body []byte) ([]member, error) {
	// A bytes.Buffer, which jsontext parses in place, so each value below is a slice of body.
	dec := jsontext.NewDecoder(bytes.NewBuffer(body), rawOptions()...)

	open, err := dec.ReadToken()
	if err != nil {
		return nil, err
	}

	if open.Kind() != '{' {
		return nil, errNotObject
	}

	var members []member

	for dec.PeekKind() != '}' {
		token, readErr := dec.ReadToken()
		if readErr != nil {
			return nil, readErr
		}

		name, start := token.String(), dec.InputOffset()

		if err = dec.SkipValue(); err != nil {
			return nil, err
		}

		members = append(members, member{name: name, value: valueAt(body, start, dec.InputOffset())})
	}

	if _, err = dec.ReadToken(); err != nil {
		return nil, err
	}

	if _, err = dec.ReadToken(); !errors.Is(err, io.EOF) {
		return nil, errAfterObject
	}

	return members, nil
}

func valueAt(body []byte, start, end int64) jsontext.Value {
	raw := bytes.TrimLeft(body[start:end], " \t\r\n")

	return bytes.TrimLeft(bytes.TrimPrefix(raw, []byte(":")), " \t\r\n")
}

type member struct {
	name  string
	value jsontext.Value
}

func named(name string) func(member) bool {
	return func(m member) bool {
		return m.name == name
	}
}

func writeMembers(members []member) ([]byte, error) {
	// Sized from the members, which an image body makes large, so the buffer never doubles.
	size, err := encodedSize(members)
	if err != nil {
		size = 0
	}

	out := encodeBuffer(size)

	enc := jsontext.NewEncoder(out, rawOptions()...)
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return nil, err
	}

	for _, m := range members {
		if err := enc.WriteToken(jsontext.String(m.name)); err != nil {
			return nil, err
		}

		if err := enc.WriteValue(m.value); err != nil {
			return nil, err
		}
	}

	if err := enc.WriteToken(jsontext.EndObject); err != nil {
		return nil, err
	}

	return withoutNewline(out.Bytes()), nil
}

func withoutNewline(encoded []byte) []byte {
	// jsontext.Encoder ends each top level value with a newline, which a request body does not need.
	return bytes.TrimSuffix(encoded, []byte("\n"))
}

func encodedSize(members []member) (int, error) {
	size := len("{}") + max(len(members)-1, 0)

	for _, m := range members {
		name, err := jsontext.AppendQuote(nil, m.name)
		if err != nil {
			return 0, unencodable(err)
		}

		size += len(name) + len(":") + len(m.value)
	}

	return size, nil
}

func rawOptions() []jsontext.Options {
	return []jsontext.Options{jsontext.AllowDuplicateNames(true), jsontext.PreserveRawStrings(true)}
}

func encodeBuffer(size int) *bytes.Buffer {
	// A jsontext encoder writing into a bytes.Buffer keeps a quarter of what it wrote free, and grows the
	// buffer, doubling it, whenever it is not. So the buffer holds that quarter from the start.
	return bytes.NewBuffer(make([]byte, 0, size+size/4+64))
}
