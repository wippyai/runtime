package data_converter

import (
	"github.com/ponyruntime/pony/pkg/payload/json"
	"github.com/ponyruntime/pony/pkg/payload/yaml"
	"strconv"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	transcoder "github.com/ponyruntime/pony/pkg/payload"
	"github.com/stretchr/testify/assert"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
)

func TestInternalDataConverter_PayloadsHandling(t *testing.T) {
	dtt := transcoder.NewTranscoder()
	json.Register(dtt)
	yaml.Register(dtt)

	defaultConverter := converter.GetDefaultDataConverter()
	conv := NewDataConverter(dtt, defaultConverter)

	t.Run("ToPayloads with single payload.Payloads", func(t *testing.T) {
		// Create test payloads
		payloads := payload.Payloads{
			payload.NewPayload([]byte("test1"), payload.JSON),
			payload.NewPayload([]byte("test2"), payload.JSON),
		}

		// Convert to Temporal payloads
		result, err := conv.ToPayloads(payloads)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Len(t, result.Payloads, 2)

		// Verify the payloads were converted correctly
		for i, p := range result.Payloads {
			assert.Equal(t, []byte(converter.MetadataEncodingJSON), p.Metadata[converter.MetadataEncoding])
			assert.Equal(t, []byte("test"+strconv.Itoa(i+1)), p.Data)
		}
	})

	t.Run("FromPayloads with payload.Payloads pointer", func(t *testing.T) {
		// Create test Temporal payloads
		input := &commonpb.Payloads{
			Payloads: []*commonpb.Payload{
				{
					Metadata: map[string][]byte{
						converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
					},
					Data: []byte("test1"),
				},
				{
					Metadata: map[string][]byte{
						converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
					},
					Data: []byte("test2"),
				},
			},
		}

		// Convert back to payload.Payloads
		var result payload.Payloads
		err := conv.FromPayloads(input, &result)
		assert.NoError(t, err)
		assert.Len(t, result, 2)

		// Verify the conversion was correct
		assert.Equal(t, payload.JSON, result[0].Format())
		assert.Equal(t, []byte("test1"), result[0].Data())
		assert.Equal(t, payload.JSON, result[1].Format())
		assert.Equal(t, []byte("test2"), result[1].Data())
	})

	t.Run("ToPayloads with empty payload.Payloads", func(t *testing.T) {
		empty := payload.Payloads{}
		result, err := conv.ToPayloads(empty)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Len(t, result.Payloads, 0)
	})

	t.Run("FromPayloads with empty Temporal payloads", func(t *testing.T) {
		input := &commonpb.Payloads{
			Payloads: []*commonpb.Payload{},
		}
		var result payload.Payloads
		err := conv.FromPayloads(input, &result)
		assert.NoError(t, err)
		assert.Len(t, result, 0)
	})

	t.Run("ToPayloads with mixed formats", func(t *testing.T) {
		payloads := payload.Payloads{
			payload.NewPayload([]byte("test1"), payload.JSON),
			payload.NewPayload([]byte("test2"), payload.Bytes),
			payload.New(nil), // nil payload
		}

		result, err := conv.ToPayloads(payloads)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Len(t, result.Payloads, 3)

		// Verify JSON payload
		assert.Equal(t, []byte(converter.MetadataEncodingJSON),
			result.Payloads[0].Metadata[converter.MetadataEncoding])
		assert.Equal(t, []byte("test1"), result.Payloads[0].Data)

		// Verify Binary payload
		assert.Equal(t, []byte(converter.MetadataEncodingBinary),
			result.Payloads[1].Metadata[converter.MetadataEncoding])
		assert.Equal(t, []byte("test2"), result.Payloads[1].Data)

		// Verify nil payload
		assert.Equal(t, []byte(converter.MetadataEncodingNil),
			result.Payloads[2].Metadata[converter.MetadataEncoding])
		assert.Nil(t, result.Payloads[2].Data)
	})

	t.Run("FromPayloads with mixed formats", func(t *testing.T) {
		input := &commonpb.Payloads{
			Payloads: []*commonpb.Payload{
				{
					Metadata: map[string][]byte{
						converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
					},
					Data: []byte("test1"),
				},
				{
					Metadata: map[string][]byte{
						converter.MetadataEncoding: []byte(converter.MetadataEncodingBinary),
					},
					Data: []byte("test2"),
				},
				{
					Metadata: map[string][]byte{
						converter.MetadataEncoding: []byte(converter.MetadataEncodingNil),
					},
				},
			},
		}

		var result payload.Payloads
		err := conv.FromPayloads(input, &result)
		assert.NoError(t, err)
		assert.Len(t, result, 3)

		// Verify JSON payload
		assert.Equal(t, payload.JSON, result[0].Format())
		assert.Equal(t, []byte("test1"), result[0].Data())

		// Verify Binary payload
		assert.Equal(t, payload.Bytes, result[1].Format())
		assert.Equal(t, []byte("test2"), result[1].Data())

		// Verify nil payload
		assert.Equal(t, payload.Golang, result[2].Format())
		assert.Nil(t, result[2].Data())
	})
}

func TestInternalDataConverter_ErrorCases(t *testing.T) {
	dtt := transcoder.NewTranscoder()
	json.Register(dtt)
	yaml.Register(dtt)

	defaultConverter := converter.GetDefaultDataConverter()
	conv := NewDataConverter(dtt, defaultConverter)

	t.Run("FromPayloads with wrong target type", func(t *testing.T) {
		input := &commonpb.Payloads{
			Payloads: []*commonpb.Payload{
				{
					Metadata: map[string][]byte{
						converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
					},
					Data: []byte("test"),
				},
			},
		}

		var wrongType string // Not a *payload.Payloads
		err := conv.FromPayloads(input, &wrongType)
		assert.Error(t, err)
	})

	t.Run("ToPayloads with unsupported format", func(t *testing.T) {
		payloads := payload.Payloads{
			payload.NewPayload("test", "unsupported/format"),
		}

		_, err := conv.ToPayloads(payloads)
		assert.Error(t, err)
	})

	t.Run("FromPayloads with mismatched length", func(t *testing.T) {
		input := &commonpb.Payloads{
			Payloads: []*commonpb.Payload{
				{
					Metadata: map[string][]byte{
						converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
					},
					Data: []byte("test1"),
				},
				{
					Metadata: map[string][]byte{
						converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
					},
					Data: []byte("test2"),
				},
			},
		}

		var str1, str2, str3 string // More targets than payloads
		err := conv.FromPayloads(input, &str1, &str2, &str3)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "number of payloads")
	})
}
