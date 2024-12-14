package yaml

import (
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"gopkg.in/yaml.v3"
)

func Register(transcoder payload.Transcoder) {
	to := &ToGolang{}
	from := &FromGolang{}

	transcoder.RegisterTranscoder(payload.Yaml, payload.Golang, 1, to)
	transcoder.RegisterTranscoder(payload.Golang, payload.Yaml, 1, from)
	transcoder.RegisterUnmarshaler(payload.Yaml, to)
}

// ToGolang converts a YAML payload to a Golang payload.
// It also implements the payload.Unmarshaler interface for YAML payloads.
type ToGolang struct{}

// Transcode implements the payload.FormatTranscoder interface.
func (t *ToGolang) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Yaml {
		return nil, fmt.Errorf("Yaml=>Golang can only transcode from YAML format, got %s", p.Format())
	}

	var data interface{}
	switch v := p.Data().(type) {
	case string:
		err := yaml.Unmarshal([]byte(v), &data)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal YAML string: %w", err)
		}
	case []byte:
		err := yaml.Unmarshal(v, &data)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal YAML bytes: %w", err)
		}
	default:
		return nil, fmt.Errorf("Yaml=>Golang can only handle string or []byte, got %T", p.Data())
	}
	return payload.NewPayload(data, payload.Golang), nil
}

// Unmarshal implements the payload.Unmarshaler interface.
func (t *ToGolang) Unmarshal(p payload.Payload, v interface{}) error {
	if p.Format() != payload.Yaml {
		return fmt.Errorf("Yaml=>Golang can only unmarshal from YAML format, got %s", p.Format())
	}

	var data []byte
	switch d := p.Data().(type) {
	case string:
		data = []byte(d)
	case []byte:
		data = d
	default:
		return fmt.Errorf("Yaml=>Golang can only unmarshal string or []byte, got %T", p.Data())
	}

	return yaml.Unmarshal(data, v)
}

// FromGolang converts a Golang payload to a YAML payload.
type FromGolang struct{}

// Transcode implements the payload.FormatTranscoder interface.
func (t *FromGolang) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Golang {
		return nil, fmt.Errorf("Golang=>Yaml can only transcode from Golang format, got %s", p.Format())
	}

	yamlData, err := yaml.Marshal(p.Data())
	if err != nil {
		return nil, fmt.Errorf("failed to marshal to YAML: %w", err)
	}

	return payload.NewPayload(string(yamlData), payload.Yaml), nil
}
