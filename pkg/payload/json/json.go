package json

import (
	"encoding/json"
	"fmt"

	"github.com/ponyruntime/pony/api/payload"
)

func Register(transcoder payload.TranscoderRegister) {
	to := &ToGolang{}
	from := &FromGolang{}

	transcoder.RegisterTranscoder(payload.Json, payload.Golang, 1, to)
	transcoder.RegisterTranscoder(payload.Golang, payload.Json, 1, from)
	transcoder.RegisterUnmarshaler(payload.Json, to)
}

// ToGolang converts a JSON payload to a Golang payload.
// It also implements the payload.Unmarshaler interface for JSON payloads.
type ToGolang struct{}

// Transcode implements the payload.FormatTranscoder interface.
func (t *ToGolang) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Json {
		return nil, fmt.Errorf("Json=>Golang can only transcode from JSON format, got %s", p.Format())
	}

	var data interface{}
	switch v := p.Data().(type) {
	case string:
		err := json.Unmarshal([]byte(v), &data)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal JSON string: %w", err)
		}
	case []byte:
		err := json.Unmarshal(v, &data)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal JSON bytes: %w", err)
		}
	default:
		return nil, fmt.Errorf("Json=>Golang can only handle string or []byte, got %T", p.Data())
	}

	return payload.NewPayload(data, payload.Golang), nil
}

// Unmarshal implements the payload.Unmarshaler interface.
func (t *ToGolang) Unmarshal(p payload.Payload, v interface{}) error {
	if p.Format() != payload.Json {
		return fmt.Errorf("Json=>Golang can only unmarshal from JSON format, got %s", p.Format())
	}

	var data []byte
	switch d := p.Data().(type) {
	case string:
		data = []byte(d)
	case []byte:
		data = d
	default:
		return fmt.Errorf("Json=>Golang can only unmarshal string or []byte, got %T", p.Data())
	}

	return json.Unmarshal(data, v)
}

// FromGolang converts a Golang payload to a JSON payload.
type FromGolang struct{}

// Transcode implements the payload.FormatTranscoder interface.
func (t *FromGolang) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Golang {
		return nil, fmt.Errorf("Golang=>Json can only transcode from Golang format, got %s", p.Format())
	}

	jsonData, err := json.Marshal(p.Data())
	if err != nil {
		return nil, fmt.Errorf("failed to marshal to JSON: %w", err)
	}

	return payload.NewPayload(jsonData, payload.Json), nil
}
