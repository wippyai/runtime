package json

import (
	"encoding/json"
	"fmt"

	"github.com/wippyai/runtime/api/payload"
)

// Note: fmt kept for Sprintf with %T

// Register registers JSON transcoders.
func Register(transcoder payload.TranscoderRegister) {
	to := &ToGolang{}
	from := &FromGolang{}

	transcoder.RegisterTranscoder(payload.JSON, payload.Golang, 1, to)
	transcoder.RegisterTranscoder(payload.Golang, payload.JSON, 1, from)
	transcoder.RegisterUnmarshaler(payload.JSON, to)
}

// ToGolang converts a JSON payload to a Golang payload.
// It also implements the payload.Unmarshaler interface for JSON payloads.
type ToGolang struct{}

// Transcode implements the payload.FormatTranscoder interface.
func (t *ToGolang) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.JSON {
		return nil, NewInvalidFormatError("JSON=>Golang", payload.JSON, p.Format())
	}

	var data interface{}
	switch v := p.Data().(type) {
	case string:
		err := json.Unmarshal([]byte(v), &data)
		if err != nil {
			return nil, NewUnmarshalError("string", err)
		}
	case []byte:
		err := json.Unmarshal(v, &data)
		if err != nil {
			return nil, NewUnmarshalError("bytes", err)
		}
	default:
		return nil, NewInvalidDataTypeError("JSON=>Golang", fmt.Sprintf("%T", p.Data()))
	}

	return payload.NewPayload(data, payload.Golang), nil
}

// Unmarshal implements the payload.Unmarshaler interface.
func (t *ToGolang) Unmarshal(p payload.Payload, v interface{}) error {
	if p.Format() != payload.JSON {
		return NewInvalidFormatError("JSON=>Golang", payload.JSON, p.Format())
	}

	var data []byte
	switch d := p.Data().(type) {
	case string:
		data = []byte(d)
	case []byte:
		data = d
	default:
		return NewInvalidDataTypeError("JSON=>Golang", fmt.Sprintf("%T", p.Data()))
	}

	return json.Unmarshal(data, v)
}

// FromGolang converts a Golang payload to a JSON payload.
type FromGolang struct{}

// Transcode implements the payload.FormatTranscoder interface.
func (t *FromGolang) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Golang {
		return nil, NewInvalidFormatError("Golang=>JSON", payload.Golang, p.Format())
	}

	jsonData, err := json.Marshal(p.Data())
	if err != nil {
		return nil, NewMarshalError(err)
	}

	return payload.NewPayload(jsonData, payload.JSON), nil
}
