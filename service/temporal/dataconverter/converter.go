package dataconverter

import (
	"fmt"

	"github.com/wippyai/runtime/api/payload"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
)

var _ converter.DataConverter = (*DataConverter)(nil)

// DataConverter implements converter.DataConverter interface for internal payloads.
type DataConverter struct {
	dtt        payload.Transcoder
	wireFormat payload.Format
}

// NewDataConverter creates a new data converter that handles internal payloads.
func NewDataConverter(dtt payload.Transcoder) converter.DataConverter {
	return &DataConverter{
		dtt:        dtt,
		wireFormat: payload.JSON,
	}
}

// ToPayloads converts a list of values to Temporal payloads.
func (c *DataConverter) ToPayloads(values ...any) (*commonpb.Payloads, error) {
	if len(values) == 0 {
		return &commonpb.Payloads{}, nil
	}

	// Special handling for payload.Messages
	if len(values) == 1 {
		if payloads, ok := values[0].(payload.Payloads); ok {
			result := &commonpb.Payloads{
				Payloads: make([]*commonpb.Payload, len(payloads)),
			}
			for i, p := range payloads {
				converted, err := c.ToPayload(p)
				if err != nil {
					return nil, fmt.Errorf("error converting payload at index %d: %w", i, err)
				}
				result.Payloads[i] = converted
			}
			return result, nil
		}
	}

	result := &commonpb.Payloads{
		Payloads: make([]*commonpb.Payload, len(values)),
	}

	for i, value := range values {
		p, err := c.ToPayload(value)
		if err != nil {
			return nil, fmt.Errorf("error converting value at index %d: %w", i, err)
		}
		result.Payloads[i] = p
	}

	return result, nil
}

// FromPayloads converts Temporal payloads to a list of values.
func (c *DataConverter) FromPayloads(payloads *commonpb.Payloads, valuePtrs ...any) error {
	if payloads == nil {
		return nil
	}

	// Special handling for payload.Payloads pointer (named type)
	if len(valuePtrs) == 1 {
		if ptr, ok := valuePtrs[0].(*payload.Payloads); ok {
			*ptr = make(payload.Payloads, len(payloads.Payloads))
			for i, p := range payloads.Payloads {
				var pload payload.Payload
				if err := c.FromPayload(p, &pload); err != nil {
					return fmt.Errorf("error converting payload at index %d: %w", i, err)
				}
				(*ptr)[i] = pload
			}
			return nil
		}

		// Special handling for *[]payload.Payload (underlying type)
		if ptr, ok := valuePtrs[0].(*[]payload.Payload); ok {
			*ptr = make([]payload.Payload, len(payloads.Payloads))
			for i, p := range payloads.Payloads {
				var pload payload.Payload
				if err := c.FromPayload(p, &pload); err != nil {
					return fmt.Errorf("error converting payload at index %d: %w", i, err)
				}
				(*ptr)[i] = pload
			}
			return nil
		}
	}

	if len(payloads.Payloads) != len(valuePtrs) {
		return fmt.Errorf(
			"number of payloads (%d) does not match number of values (%d)",
			len(payloads.Payloads), len(valuePtrs),
		)
	}

	for i, p := range payloads.Payloads {
		err := c.FromPayload(p, valuePtrs[i])
		if err != nil {
			return fmt.Errorf("error converting payload at index %d: %w", i, err)
		}
	}

	return nil
}

// ToPayload converts a single value to Temporal payload.
func (c *DataConverter) ToPayload(value any) (*commonpb.Payload, error) {
	if raw, ok := value.(converter.RawValue); ok {
		return raw.Payload(), nil
	}

	pValue, ok := value.(payload.Payload)
	if !ok {
		pValue = payload.New(value)
	}

	return c.toTemporalPayload(pValue)
}

// FromPayload converts a Temporal payload to a value.
func (c *DataConverter) FromPayload(p *commonpb.Payload, valuePtr any) error {
	if p == nil {
		return nil
	}

	if raw, ok := valuePtr.(*converter.RawValue); ok {
		*raw = converter.NewRawValue(p)
		return nil
	}

	if ptr, ok := valuePtr.(*payload.Payload); ok {
		converted, err := c.fromTemporalPayload(p)
		if err != nil {
			return err
		}
		*ptr = converted
		return nil
	}

	converted, err := c.fromTemporalPayload(p)
	if err != nil {
		return err
	}

	return c.dtt.Unmarshal(converted, valuePtr)
}

// ToString converts payload to string.
func (c *DataConverter) ToString(p *commonpb.Payload) string {
	if p == nil {
		return ""
	}
	enc, err := encoding(p)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("%s (%d bytes)", enc, len(p.Data))
}

// ToStrings converts payloads to strings.
func (c *DataConverter) ToStrings(payloads *commonpb.Payloads) []string {
	if payloads == nil {
		return nil
	}

	out := make([]string, 0, len(payloads.Payloads))
	for _, pl := range payloads.Payloads {
		out = append(out, c.ToString(pl))
	}
	return out
}

func (c *DataConverter) toTemporalPayload(pValue payload.Payload) (*commonpb.Payload, error) {
	if pValue == nil {
		return nil, fmt.Errorf("nil payload value")
	}

	if pValue.Data() == nil {
		return &commonpb.Payload{
			Metadata: map[string][]byte{
				converter.MetadataEncoding: []byte(converter.MetadataEncodingNil),
			},
		}, nil
	}

	// Canonical boundary passthrough only: bytes and JSON.
	switch pValue.Format() {
	case payload.Bytes:
		data, err := bytesData(pValue.Data())
		if err != nil {
			return nil, err
		}
		return &commonpb.Payload{
			Metadata: map[string][]byte{
				converter.MetadataEncoding: []byte(converter.MetadataEncodingBinary),
			},
			Data: data,
		}, nil
	case payload.JSON:
		data, err := bytesData(pValue.Data())
		if err != nil {
			return nil, err
		}
		return &commonpb.Payload{
			Metadata: map[string][]byte{
				converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
			},
			Data: data,
		}, nil
	}

	// Canonicalize all non-canonical payloads through shared transcoder.
	converted, err := c.dtt.Transcode(pValue, c.wireFormat)
	if err != nil {
		return nil, fmt.Errorf("error transcoding value: %w", err)
	}
	if converted.Format() != c.wireFormat {
		return nil, fmt.Errorf("transcoded to unexpected format: %s", converted.Format())
	}

	data, err := bytesData(converted.Data())
	if err != nil {
		return nil, err
	}

	return &commonpb.Payload{
		Metadata: map[string][]byte{
			converter.MetadataEncoding: []byte(c.encoding()),
		},
		Data: data,
	}, nil
}

func (c *DataConverter) fromTemporalPayload(p *commonpb.Payload) (payload.Payload, error) {
	enc, err := encoding(p)
	if err != nil {
		return nil, err
	}

	switch enc {
	case converter.MetadataEncodingNil:
		return payload.New(nil), nil
	case converter.MetadataEncodingBinary:
		return payload.NewPayload(p.Data, payload.Bytes), nil
	case converter.MetadataEncodingJSON:
		return payload.NewPayload(p.Data, payload.JSON), nil
	default:
		return nil, fmt.Errorf("unsupported temporal payload encoding: %s", enc)
	}
}

func encoding(p *commonpb.Payload) (string, error) {
	if p.Metadata == nil {
		return "", fmt.Errorf("missing encoding metadata in payload")
	}
	enc, ok := p.Metadata[converter.MetadataEncoding]
	if !ok {
		return "", fmt.Errorf("missing encoding metadata in payload")
	}
	return string(enc), nil
}

func bytesData(data any) ([]byte, error) {
	switch v := data.(type) {
	case nil:
		return nil, nil
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("payload data is not []byte or string")
	}
}

func (c *DataConverter) encoding() string {
	switch c.wireFormat {
	case payload.JSON:
		return converter.MetadataEncodingJSON
	case payload.Bytes:
		return converter.MetadataEncodingBinary
	default:
		return c.wireFormat
	}
}
