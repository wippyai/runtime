package yaml

import (
	"fmt"

	"github.com/wippyai/runtime/api/payload"
	"gopkg.in/yaml.v3"
)

// Note: fmt kept for Sprintf with %T

// Register registers the YAML transcoders.
func Register(transcoder payload.TranscoderRegister) {
	to := &ToGolang{}
	from := &FromGolang{}

	transcoder.RegisterTranscoder(payload.YAML, payload.Golang, 2, to)
	transcoder.RegisterTranscoder(payload.Golang, payload.YAML, 2, from)
	transcoder.RegisterUnmarshaler(payload.YAML, to)
}

// ToGolang converts a YAML payload to a Golang payload.
// It also implements the payload.Unmarshaler interface for YAML payloads.
type ToGolang struct{}

// Transcode implements the payload.FormatTranscoder interface.
func (t *ToGolang) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.YAML {
		return nil, NewInvalidFormatError("YAML=>Golang", payload.YAML, p.Format())
	}

	var data interface{}
	switch v := p.Data().(type) {
	case string:
		err := yaml.Unmarshal([]byte(v), &data)
		if err != nil {
			return nil, NewUnmarshalError("string", err)
		}
	case []byte:
		err := yaml.Unmarshal(v, &data)
		if err != nil {
			return nil, NewUnmarshalError("bytes", err)
		}
	default:
		return nil, NewInvalidDataTypeError("YAML=>Golang", fmt.Sprintf("%T", p.Data()))
	}
	return payload.NewPayload(data, payload.Golang), nil
}

// Unmarshal implements the payload.Unmarshaler interface.
func (t *ToGolang) Unmarshal(p payload.Payload, v interface{}) error {
	if p.Format() != payload.YAML {
		return NewInvalidFormatError("YAML=>Golang", payload.YAML, p.Format())
	}

	var data []byte
	switch d := p.Data().(type) {
	case string:
		data = []byte(d)
	case []byte:
		data = d
	default:
		return NewInvalidDataTypeError("YAML=>Golang", fmt.Sprintf("%T", p.Data()))
	}

	return yaml.Unmarshal(data, v)
}

// FromGolang converts a Golang payload to a YAML payload.
type FromGolang struct{}

// Transcode implements the payload.FormatTranscoder interface.
func (t *FromGolang) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Golang {
		return nil, NewInvalidFormatError("Golang=>YAML", payload.Golang, p.Format())
	}

	yamlData, err := yaml.Marshal(p.Data())
	if err != nil {
		return nil, NewMarshalError(err)
	}

	return payload.NewPayload(string(yamlData), payload.YAML), nil
}
