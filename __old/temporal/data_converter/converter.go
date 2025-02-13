package data_converter

import (
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
)

// DataConverter implements converter.DataConverter interface for internal payloads.
type DataConverter struct {
	dtt      payload.Transcoder
	fallback converter.DataConverter
}

// NewDataConverter creates a new data converter that handles internal payloads.
func NewDataConverter(
	dtt payload.Transcoder,
	fallback converter.DataConverter) converter.DataConverter {
	return &DataConverter{
		dtt:      dtt,
		fallback: fallback,
	}
}

// ToPayloads converts a list of values to Temporal payloads.
func (c *DataConverter) ToPayloads(values ...any) (*commonpb.Payloads, error) {
	if len(values) == 0 {
		return nil, nil
	}

	// Special handling for payload.Payloads
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

	// Special handling for payload.Payloads pointer
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
	// check if our value instance of payload.Payload
	pValue, ok := value.(payload.Payload)
	if !ok {
		return c.fallback.ToPayload(value)
	}

	// we can handle nil, json and bytes directly without transcoding
	if pValue.Data() == nil {
		return &commonpb.Payload{
			Metadata: map[string][]byte{
				converter.MetadataEncoding: []byte(converter.MetadataEncodingNil),
			},
		}, nil
	}

	if pValue.Format() == payload.Bytes {
		return &commonpb.Payload{
				Metadata: map[string][]byte{
					converter.MetadataEncoding: []byte(converter.MetadataEncodingBinary),
				},
				Data: pValue.Data().([]byte),
			},
			nil
	}

	if pValue.Format() == payload.JSON {
		return &commonpb.Payload{
				Metadata: map[string][]byte{
					converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
				},
				Data: pValue.Data().([]byte),
			},
			nil
	}

	// we need some common format, and for now it's JSON
	jValue, err := c.dtt.Transcode(pValue, payload.JSON)
	if err != nil {
		return nil, fmt.Errorf("error transcoding value: %w", err)
	}

	// json contract
	data, ok := jValue.Data().([]byte)
	if !ok {
		return nil, fmt.Errorf("error converting data to []byte")
	}

	// Handle internal payload type
	return &commonpb.Payload{
		Metadata: map[string][]byte{
			converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
		},
		Data: data,
	}, nil
}

// FromPayload converts a Temporal payload to a value.
func (c *DataConverter) FromPayload(p *commonpb.Payload, valuePtr any) error {
	if p == nil {
		return nil
	}

	// are we trying to convert to payload.Payload?
	ptr, ok := valuePtr.(*payload.Payload)
	if !ok {
		return c.fallback.FromPayload(p, valuePtr)
	}

	// we only support JSON encoding for now
	enc, ok := p.Metadata[converter.MetadataEncoding]
	if !ok {
		return fmt.Errorf("unsupported paylolad to payload encoding %s", enc)
	}

	switch {
	case string(enc) == converter.MetadataEncodingJSON:
		// we only support JSON encoding for now
		*ptr = payload.NewPayload(p.Data, payload.JSON)
		return nil
	case string(enc) == converter.MetadataEncodingNil:
		*ptr = payload.New(nil)
		return nil
	case string(enc) == converter.MetadataEncodingBinary:
		*ptr = payload.NewPayload(p.Data, payload.Bytes)
		return nil
	}

	// Fallback for other types
	return c.fallback.FromPayload(p, valuePtr)
}

// ToString converts payload to string.
func (c *DataConverter) ToString(p *commonpb.Payload) string {
	return c.fallback.ToString(p)
}

// ToStrings converts payloads to strings.
func (c *DataConverter) ToStrings(payloads *commonpb.Payloads) []string {
	return c.fallback.ToStrings(payloads)
}
