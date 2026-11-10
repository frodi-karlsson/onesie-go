package onesie

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
)

var bodyOptions = jsonv2.JoinOptions(
	jsonv2.Deterministic(true),
	// A raw state keeps a repeated name as written, as a raw body does, since a provider may accept it.
	jsontext.AllowDuplicateNames(true),
	jsonv2.WithMarshalers(jsonv2.MarshalToFunc(writeQuestions)),
)

func marshalWith(write func(*jsontext.Encoder) error) ([]byte, error) {
	var buf bytes.Buffer

	enc := jsontext.NewEncoder(&buf, bodyOptions)
	if err := write(enc); err != nil {
		return nil, err
	}

	return withoutNewline(buf.Bytes()), nil
}

func writeQuestions(enc *jsontext.Encoder, questions Questions) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}

	for _, named := range questions {
		if err := enc.WriteToken(jsontext.String(named.ID)); err != nil {
			return err
		}

		if err := writeQuestion(enc, named.Question); err != nil {
			return err
		}
	}

	return enc.WriteToken(jsontext.EndObject)
}

func writeQuestion(enc *jsontext.Encoder, question Question) error {
	if isNil(question) {
		return enc.WriteToken(jsontext.Null)
	}

	switch typed := dereferenced(question).(type) {
	case Noul:
		return writeNoul(enc, typed)
	case Choice:
		return writeChoice(enc, typed)
	case Score:
		return writeScore(enc, typed)
	case Box:
		return writeBox(enc, typed)
	default:
		return jsonv2.MarshalEncode(enc, question)
	}
}

func writeNoul(enc *jsontext.Encoder, q Noul) error {
	return writeObject(enc, func() error {
		if err := writeMember(enc, "type", "noul"); err != nil {
			return err
		}

		if err := writeMember(enc, "instructions", q.Instructions); err != nil {
			return err
		}

		if q.Criteria == nil {
			return nil
		}

		return writeMember(enc, "criteria", q.Criteria)
	})
}

func writeChoice(enc *jsontext.Encoder, q Choice) error {
	return writeObject(enc, func() error {
		if err := writeMember(enc, "type", "choice"); err != nil {
			return err
		}

		if err := writeMember(enc, "instructions", q.Instructions); err != nil {
			return err
		}

		if err := enc.WriteToken(jsontext.String("criteria")); err != nil {
			return err
		}

		return writeCriteria(enc, q.Criteria)
	})
}

func writeCriteria(enc *jsontext.Encoder, criteria Criteria) error {
	if criteria == nil {
		return enc.WriteToken(jsontext.Null)
	}

	return writeObject(enc, func() error {
		for _, named := range criteria {
			if err := writeMember(enc, named.Name, named.Desc); err != nil {
				return err
			}
		}

		return nil
	})
}

func writeScore(enc *jsontext.Encoder, q Score) error {
	return writeObject(enc, func() error {
		if err := writeMember(enc, "type", "score"); err != nil {
			return err
		}

		if err := writeMember(enc, "instructions", q.Instructions); err != nil {
			return err
		}

		return writeMember(enc, "criteria", q.Criteria)
	})
}

func writeBox(enc *jsontext.Encoder, q Box) error {
	return writeObject(enc, func() error {
		if err := writeMember(enc, "type", "box"); err != nil {
			return err
		}

		return writeMember(enc, "instructions", q.Instructions)
	})
}

func writeObject(enc *jsontext.Encoder, members func() error) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}

	if err := members(); err != nil {
		return err
	}

	return enc.WriteToken(jsontext.EndObject)
}

func writeMember(enc *jsontext.Encoder, name string, value any) error {
	if err := enc.WriteToken(jsontext.String(name)); err != nil {
		return err
	}

	return jsonv2.MarshalEncode(enc, value)
}
