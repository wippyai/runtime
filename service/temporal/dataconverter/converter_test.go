package dataconverter

import (
	"strconv"
	"testing"

	jpayload "github.com/wippyai/runtime/system/payload/json"
	msgpayload "github.com/wippyai/runtime/system/payload/msgpack"
	ypayload "github.com/wippyai/runtime/system/payload/yaml"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	transcoder "github.com/wippyai/runtime/system/payload"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
)

func TestInternalDataConverter_PayloadsHandling(t *testing.T) {
	dtt := transcoder.NewTranscoder()
	jpayload.Register(dtt)
	msgpayload.Register(dtt)
	ypayload.Register(dtt)

	conv := NewDataConverter(dtt)

	t.Run("ToPayloads with single payload.Messages", func(t *testing.T) {
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

	t.Run("FromPayloads with payload.Messages pointer", func(t *testing.T) {
		// Create test Temporal payloads with valid JSON
		input := &commonpb.Payloads{
			Payloads: []*commonpb.Payload{
				{
					Metadata: map[string][]byte{
						converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
					},
					Data: []byte(`"test1"`),
				},
				{
					Metadata: map[string][]byte{
						converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
					},
					Data: []byte(`"test2"`),
				},
			},
		}

		// Convert back to payload.Messages
		var result payload.Payloads
		err := conv.FromPayloads(input, &result)
		assert.NoError(t, err)
		assert.Len(t, result, 2)

		// Verify the conversion was correct - transcoded to JSON format
		assert.Equal(t, payload.JSON, result[0].Format())
		assert.Equal(t, payload.JSON, result[1].Format())
	})

	t.Run("ToPayloads with empty payload.Messages", func(t *testing.T) {
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
					Data: []byte(`"test1"`),
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

		// Verify JSON payload - transcoded to JSON format
		assert.Equal(t, payload.JSON, result[0].Format())

		// Verify Binary payload - stays as bytes
		assert.Equal(t, payload.Bytes, result[1].Format())
		assert.Equal(t, []byte("test2"), result[1].Data())

		// Verify nil payload
		assert.Equal(t, payload.Golang, result[2].Format())
		assert.Nil(t, result[2].Data())
	})
}

func TestInternalDataConverter_ErrorCases(t *testing.T) {
	dtt := transcoder.NewTranscoder()
	jpayload.Register(dtt)
	msgpayload.Register(dtt)
	ypayload.Register(dtt)

	conv := NewDataConverter(dtt)

	t.Run("FromPayloads with wrong target type", func(t *testing.T) {
		input := &commonpb.Payloads{
			Payloads: []*commonpb.Payload{
				{
					Metadata: map[string][]byte{
						converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
					},
					Data: []byte(`{"key": "value"}`),
				},
			},
		}

		// Passing an incompatible type (int can't decode JSON object)
		var wrongType int
		err := conv.FromPayloads(input, &wrongType)
		assert.Error(t, err)
	})

	t.Run("ToPayloads with unsupported non-wire format", func(t *testing.T) {
		payloads := payload.Payloads{
			payload.NewPayload(map[string]any{"key": "value"}, "unsupported/format"),
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
					Data: []byte(`"test1"`),
				},
				{
					Metadata: map[string][]byte{
						converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
					},
					Data: []byte(`"test2"`),
				},
			},
		}

		var str1, str2, str3 string // More targets than payloads
		err := conv.FromPayloads(input, &str1, &str2, &str3)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "number of payloads")
	})
}

func TestDataConverter_PayloadNewTranscoding(t *testing.T) {
	// Create a transcoder and register JSON support
	dtt := transcoder.NewTranscoder()
	jpayload.Register(dtt)
	msgpayload.Register(dtt)
	ypayload.Register(dtt)

	conv := NewDataConverter(dtt)

	t.Run("ToPayload with payload.New needing transcoding", func(t *testing.T) {
		// Create a payload using payload.New (will be Golang format)
		data := map[string]any{
			"key": "value",
			"num": float64(42),
		}
		p := payload.New(data)

		// Convert to Temporal payload
		result, err := conv.ToPayload(p)
		assert.NoError(t, err)
		assert.NotNil(t, result)

		// Should be transcoded to canonical JSON wire format
		assert.Equal(t, []byte(converter.MetadataEncodingJSON), result.Metadata[converter.MetadataEncoding])

		// Verify content
		decodedPayload := payload.NewPayload(result.Data, payload.JSON)
		decoded, err := dtt.Transcode(decodedPayload, payload.Golang)
		assert.NoError(t, err)
		decodedData, ok := decoded.Data().(map[string]any)
		require.True(t, ok)
		assert.Equal(t, data["key"], decodedData["key"])
		switch v := decodedData["num"].(type) {
		case int:
			assert.Equal(t, 42, v)
		case int64:
			assert.Equal(t, int64(42), v)
		case float64:
			assert.Equal(t, float64(42), v)
		default:
			t.Fatalf("unexpected num type: %T", decodedData["num"])
		}
	})

	t.Run("ToPayload with byte-compatible custom format", func(t *testing.T) {
		p := payload.NewPayload("value", payload.Format("custom"))

		_, err := conv.ToPayload(p)
		require.Error(t, err)
	})
}
