package std

import (
	"encoding/json"
	"fmt"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
)

// ParseHeader extracts and unmarshals the typed header from command params.
// The header is expected to be in Params[0].
func ParseHeader[T any](cmd runtime.Command) (*T, error) {
	params := cmd.Params()
	if len(params) == 0 {
		return nil, fmt.Errorf("command has no params")
	}

	header := new(T)
	if err := UnmarshalPayload(params[0], header); err != nil {
		return nil, fmt.Errorf("failed to parse header: %w", err)
	}

	return header, nil
}

// ExtractArgs returns the argument payloads from a command (Params[1:]).
func ExtractArgs(cmd runtime.Command) payload.Payloads {
	params := cmd.Params()
	if len(params) <= 1 {
		return nil
	}
	return params[1:]
}

// UnmarshalPayload unmarshals a payload into a target struct.
func UnmarshalPayload(p payload.Payload, target any) error {
	if p == nil {
		return fmt.Errorf("payload is nil")
	}

	data := p.Data()
	if data == nil {
		return fmt.Errorf("payload data is nil")
	}

	switch v := data.(type) {
	case []byte:
		return json.Unmarshal(v, target)
	case string:
		return json.Unmarshal([]byte(v), target)
	case map[string]any:
		bytes, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return json.Unmarshal(bytes, target)
	default:
		bytes, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("cannot marshal payload data: %w", err)
		}
		return json.Unmarshal(bytes, target)
	}
}

// MarshalHeader creates a payload from a header struct.
func MarshalHeader(header any) (payload.Payload, error) {
	return payload.New(header), nil
}
