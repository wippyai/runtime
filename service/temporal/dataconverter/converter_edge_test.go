package dataconverter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	transcoder "github.com/wippyai/runtime/system/payload"
	jpayload "github.com/wippyai/runtime/system/payload/json"
	msgpayload "github.com/wippyai/runtime/system/payload/msgpack"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
)

func newTestConverter() *DataConverter {
	dtt := transcoder.NewTranscoder()
	jpayload.Register(dtt)
	msgpayload.Register(dtt)
	return NewDataConverter(dtt).(*DataConverter)
}

// --- ToString ---

func TestDataConverter_ToString_Nil(t *testing.T) {
	c := newTestConverter()
	assert.Equal(t, "", c.ToString(nil))
}

func TestDataConverter_ToString_JSON(t *testing.T) {
	c := newTestConverter()
	p := &commonpb.Payload{
		Metadata: map[string][]byte{
			converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
		},
		Data: []byte(`{"key":"value"}`),
	}
	result := c.ToString(p)
	assert.Contains(t, result, "json/plain")
	assert.Contains(t, result, "15 bytes")
}

func TestDataConverter_ToString_Binary(t *testing.T) {
	c := newTestConverter()
	p := &commonpb.Payload{
		Metadata: map[string][]byte{
			converter.MetadataEncoding: []byte(converter.MetadataEncodingBinary),
		},
		Data: []byte{0x01, 0x02, 0x03},
	}
	result := c.ToString(p)
	assert.Contains(t, result, "binary/plain")
	assert.Contains(t, result, "3 bytes")
}

func TestDataConverter_ToString_MissingMetadata(t *testing.T) {
	c := newTestConverter()
	p := &commonpb.Payload{Data: []byte("test")}
	result := c.ToString(p)
	assert.Contains(t, result, "missing encoding metadata")
}

func TestDataConverter_ToString_NilMetadata(t *testing.T) {
	c := newTestConverter()
	p := &commonpb.Payload{
		Metadata: map[string][]byte{},
		Data:     []byte("test"),
	}
	result := c.ToString(p)
	assert.Contains(t, result, "missing encoding metadata")
}

// --- ToStrings ---

func TestDataConverter_ToStrings_Nil(t *testing.T) {
	c := newTestConverter()
	assert.Nil(t, c.ToStrings(nil))
}

func TestDataConverter_ToStrings_Empty(t *testing.T) {
	c := newTestConverter()
	result := c.ToStrings(&commonpb.Payloads{Payloads: []*commonpb.Payload{}})
	assert.Empty(t, result)
}

func TestDataConverter_ToStrings_Multiple(t *testing.T) {
	c := newTestConverter()
	payloads := &commonpb.Payloads{
		Payloads: []*commonpb.Payload{
			{
				Metadata: map[string][]byte{
					converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
				},
				Data: []byte(`"a"`),
			},
			{
				Metadata: map[string][]byte{
					converter.MetadataEncoding: []byte(converter.MetadataEncodingBinary),
				},
				Data: []byte{0x01},
			},
		},
	}

	result := c.ToStrings(payloads)
	require.Len(t, result, 2)
	assert.Contains(t, result[0], "json/plain")
	assert.Contains(t, result[1], "binary/plain")
}

// --- ToPayloads edge cases ---

func TestDataConverter_ToPayloads_Empty(t *testing.T) {
	c := newTestConverter()
	result, err := c.ToPayloads()
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.Payloads)
}

func TestDataConverter_ToPayloads_NilPayloadValue(t *testing.T) {
	c := newTestConverter()
	p := payload.New(nil)
	result, err := c.ToPayloads(p)
	require.NoError(t, err)
	assert.Len(t, result.Payloads, 1)
	assert.Equal(t, []byte(converter.MetadataEncodingNil), result.Payloads[0].Metadata[converter.MetadataEncoding])
}

func TestDataConverter_ToPayloads_NonPayloadValue(t *testing.T) {
	c := newTestConverter()
	// Passing raw Go value (not payload.Payload)
	result, err := c.ToPayloads(map[string]any{"key": "val"})
	require.NoError(t, err)
	assert.Len(t, result.Payloads, 1)
	assert.Equal(t, []byte(converter.MetadataEncodingJSON), result.Payloads[0].Metadata[converter.MetadataEncoding])
}

func TestDataConverter_ToPayloads_MsgPack(t *testing.T) {
	c := newTestConverter()
	p := payload.NewPayload([]byte{0x91, 0x01}, payload.MsgPack)
	result, err := c.ToPayloads(p)
	require.NoError(t, err)
	assert.Len(t, result.Payloads, 1)
	assert.Equal(t, []byte(converter.MetadataEncodingJSON), result.Payloads[0].Metadata[converter.MetadataEncoding])
}

// --- FromPayloads edge cases ---

func TestDataConverter_FromPayloads_NilPayloads(t *testing.T) {
	c := newTestConverter()
	err := c.FromPayloads(nil)
	assert.NoError(t, err)
}

func TestDataConverter_FromPayloads_SlicePayloadPtr(t *testing.T) {
	c := newTestConverter()
	input := &commonpb.Payloads{
		Payloads: []*commonpb.Payload{
			{
				Metadata: map[string][]byte{
					converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
				},
				Data: []byte(`"hello"`),
			},
		},
	}

	var result []payload.Payload
	err := c.FromPayloads(input, &result)
	require.NoError(t, err)
	assert.Len(t, result, 1)
}

// --- FromPayload edge cases ---

func TestDataConverter_FromPayload_NilPayload(t *testing.T) {
	c := newTestConverter()
	var p payload.Payload
	err := c.FromPayload(nil, &p)
	assert.NoError(t, err)
}

func TestDataConverter_FromPayload_PayloadPtr(t *testing.T) {
	c := newTestConverter()
	input := &commonpb.Payload{
		Metadata: map[string][]byte{
			converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
		},
		Data: []byte(`"test"`),
	}

	var result payload.Payload
	err := c.FromPayload(input, &result)
	require.NoError(t, err)
	assert.Equal(t, payload.JSON, result.Format())
}

func TestDataConverter_FromPayload_NilEncoding(t *testing.T) {
	c := newTestConverter()
	input := &commonpb.Payload{
		Metadata: map[string][]byte{
			converter.MetadataEncoding: []byte(converter.MetadataEncodingNil),
		},
	}

	var result payload.Payload
	err := c.FromPayload(input, &result)
	require.NoError(t, err)
	assert.Nil(t, result.Data())
}

func TestDataConverter_FromPayload_UnknownEncodingRejected(t *testing.T) {
	c := newTestConverter()
	input := &commonpb.Payload{
		Metadata: map[string][]byte{
			converter.MetadataEncoding: []byte("unknown/format"),
		},
		Data: []byte("data"),
	}

	var result payload.Payload
	err := c.FromPayload(input, &result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported temporal payload encoding")
}

func TestDataConverter_FromPayload_RawValue(t *testing.T) {
	c := newTestConverter()
	input := &commonpb.Payload{
		Metadata: map[string][]byte{
			converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
		},
		Data: []byte(`"raw"`),
	}

	var result converter.RawValue
	err := c.FromPayload(input, &result)
	require.NoError(t, err)
	assert.NotNil(t, result.Payload())
}

// --- ToPayload edge cases ---

func TestDataConverter_ToPayload_RawValue(t *testing.T) {
	c := newTestConverter()
	raw := converter.NewRawValue(&commonpb.Payload{
		Metadata: map[string][]byte{
			converter.MetadataEncoding: []byte("custom"),
		},
		Data: []byte("raw-data"),
	})

	result, err := c.ToPayload(raw)
	require.NoError(t, err)
	assert.Equal(t, []byte("raw-data"), result.Data)
}

func TestDataConverter_ToPayload_BytesFormat(t *testing.T) {
	c := newTestConverter()
	p := payload.NewPayload([]byte{0xDE, 0xAD}, payload.Bytes)
	result, err := c.ToPayload(p)
	require.NoError(t, err)
	assert.Equal(t, []byte(converter.MetadataEncodingBinary), result.Metadata[converter.MetadataEncoding])
	assert.Equal(t, []byte{0xDE, 0xAD}, result.Data)
}

// --- bytesData ---

func TestBytesData_Nil(t *testing.T) {
	result, err := bytesData(nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestBytesData_Bytes(t *testing.T) {
	result, err := bytesData([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), result)
}

func TestBytesData_String(t *testing.T) {
	result, err := bytesData("hello")
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), result)
}

func TestBytesData_Unsupported(t *testing.T) {
	_, err := bytesData(42)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not []byte or string")
}

// --- encoding ---

func TestEncoding_NilMetadata(t *testing.T) {
	_, err := encoding(&commonpb.Payload{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing encoding metadata")
}

func TestEncoding_MissingKey(t *testing.T) {
	_, err := encoding(&commonpb.Payload{
		Metadata: map[string][]byte{"other": []byte("x")},
	})
	assert.Error(t, err)
}

func TestEncoding_Valid(t *testing.T) {
	enc, err := encoding(&commonpb.Payload{
		Metadata: map[string][]byte{
			converter.MetadataEncoding: []byte(converter.MetadataEncodingJSON),
		},
	})
	require.NoError(t, err)
	assert.Equal(t, converter.MetadataEncodingJSON, enc)
}

// --- Roundtrip ---

func TestDataConverter_Roundtrip_JSON(t *testing.T) {
	c := newTestConverter()
	original := payload.NewPayload([]byte(`{"key":"value"}`), payload.JSON)

	temporal, err := c.ToPayload(original)
	require.NoError(t, err)

	var restored payload.Payload
	err = c.FromPayload(temporal, &restored)
	require.NoError(t, err)

	assert.Equal(t, payload.JSON, restored.Format())
	assert.Equal(t, []byte(`{"key":"value"}`), restored.Data())
}

func TestDataConverter_Roundtrip_Binary(t *testing.T) {
	c := newTestConverter()
	original := payload.NewPayload([]byte{0x01, 0x02, 0x03}, payload.Bytes)

	temporal, err := c.ToPayload(original)
	require.NoError(t, err)

	var restored payload.Payload
	err = c.FromPayload(temporal, &restored)
	require.NoError(t, err)

	assert.Equal(t, payload.Bytes, restored.Format())
	assert.Equal(t, []byte{0x01, 0x02, 0x03}, restored.Data())
}

func TestDataConverter_Roundtrip_Nil(t *testing.T) {
	c := newTestConverter()
	original := payload.New(nil)

	temporal, err := c.ToPayload(original)
	require.NoError(t, err)

	var restored payload.Payload
	err = c.FromPayload(temporal, &restored)
	require.NoError(t, err)

	assert.Nil(t, restored.Data())
}
