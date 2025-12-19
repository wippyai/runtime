package propagator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
)

func TestNew(t *testing.T) {
	p := New()
	require.NotNil(t, p)
}

func TestWithValues(t *testing.T) {
	ctx := context.Background()
	values := map[string]any{
		"key1": "value1",
		"key2": 42,
	}

	ctx = WithValues(ctx, values)
	result := GetContextValues(ctx)

	require.NotNil(t, result)
	assert.Equal(t, "value1", result["key1"])
	assert.Equal(t, 42, result["key2"])
}

func TestGetContextValues_Empty(t *testing.T) {
	ctx := context.Background()
	result := GetContextValues(ctx)
	assert.Nil(t, result)
}

func TestCreateHeader(t *testing.T) {
	t.Run("with values", func(t *testing.T) {
		values := map[string]any{
			"user": "alice",
			"role": "admin",
		}

		header, err := CreateHeader(values)
		require.NoError(t, err)
		require.NotNil(t, header)
		assert.Contains(t, header.Fields, HeaderKey)
	})

	t.Run("empty values", func(t *testing.T) {
		header, err := CreateHeader(map[string]any{})
		require.NoError(t, err)
		assert.Nil(t, header)
	})

	t.Run("nil values", func(t *testing.T) {
		header, err := CreateHeader(nil)
		require.NoError(t, err)
		assert.Nil(t, header)
	})
}

func TestExtractFromHeader(t *testing.T) {
	t.Run("with valid header", func(t *testing.T) {
		values := map[string]any{
			"user":  "alice",
			"count": float64(42), // JSON numbers become float64
		}

		header, err := CreateHeader(values)
		require.NoError(t, err)

		extracted, err := ExtractFromHeader(header)
		require.NoError(t, err)
		require.NotNil(t, extracted)
		assert.Equal(t, "alice", extracted["user"])
		assert.Equal(t, float64(42), extracted["count"])
	})

	t.Run("nil header", func(t *testing.T) {
		extracted, err := ExtractFromHeader(nil)
		require.NoError(t, err)
		assert.Nil(t, extracted)
	})

	t.Run("header without fields", func(t *testing.T) {
		header := &commonpb.Header{}
		extracted, err := ExtractFromHeader(header)
		require.NoError(t, err)
		assert.Nil(t, extracted)
	})

	t.Run("header without wippy key", func(t *testing.T) {
		header := &commonpb.Header{
			Fields: map[string]*commonpb.Payload{
				"other-key": {},
			},
		}
		extracted, err := ExtractFromHeader(header)
		require.NoError(t, err)
		assert.Nil(t, extracted)
	})

	t.Run("header with invalid JSON", func(t *testing.T) {
		payload, _ := converter.GetDefaultDataConverter().ToPayload([]byte("invalid json"))
		header := &commonpb.Header{
			Fields: map[string]*commonpb.Payload{
				HeaderKey: payload,
			},
		}
		_, err := ExtractFromHeader(header)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unmarshal")
	})
}

func TestCreateAndExtractRoundTrip(t *testing.T) {
	original := map[string]any{
		"string": "hello",
		"int":    float64(123), // JSON numbers are float64
		"bool":   true,
		"nested": map[string]any{
			"inner": "value",
		},
	}

	header, err := CreateHeader(original)
	require.NoError(t, err)

	extracted, err := ExtractFromHeader(header)
	require.NoError(t, err)

	assert.Equal(t, original["string"], extracted["string"])
	assert.Equal(t, original["int"], extracted["int"])
	assert.Equal(t, original["bool"], extracted["bool"])

	nested, ok := extracted["nested"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value", nested["inner"])
}

func TestPropagator_Inject(t *testing.T) {
	t.Run("with simple context values", func(t *testing.T) {
		p := New()
		writer := &mockHeaderWriter{fields: make(map[string]*commonpb.Payload)}

		ctx := WithValues(context.Background(), map[string]any{
			"tenant": "acme",
		})

		err := p.Inject(ctx, writer)
		require.NoError(t, err)
		assert.Contains(t, writer.fields, HeaderKey)
	})

	t.Run("empty context", func(t *testing.T) {
		p := New()
		writer := &mockHeaderWriter{fields: make(map[string]*commonpb.Payload)}

		err := p.Inject(context.Background(), writer)
		require.NoError(t, err)
		assert.NotContains(t, writer.fields, HeaderKey)
	})
}

func TestPropagator_Extract(t *testing.T) {
	t.Run("with valid header", func(t *testing.T) {
		p := New()

		// Create header with values
		values := map[string]any{"user": "bob"}
		header, _ := CreateHeader(values)

		reader := &mockHeaderReader{fields: header.Fields}

		ctx, err := p.Extract(context.Background(), reader)
		require.NoError(t, err)

		result := GetContextValues(ctx)
		require.NotNil(t, result)
		assert.Equal(t, "bob", result["user"])
	})

	t.Run("without header", func(t *testing.T) {
		p := New()
		reader := &mockHeaderReader{fields: make(map[string]*commonpb.Payload)}

		ctx, err := p.Extract(context.Background(), reader)
		require.NoError(t, err)

		result := GetContextValues(ctx)
		assert.Nil(t, result)
	})
}

// mockHeaderWriter implements workflow.HeaderWriter
type mockHeaderWriter struct {
	fields map[string]*commonpb.Payload
}

func (m *mockHeaderWriter) Set(key string, payload *commonpb.Payload) {
	m.fields[key] = payload
}

// mockHeaderReader implements workflow.HeaderReader
type mockHeaderReader struct {
	fields map[string]*commonpb.Payload
}

func (m *mockHeaderReader) Get(key string) (*commonpb.Payload, bool) {
	p, ok := m.fields[key]
	return p, ok
}

func (m *mockHeaderReader) ForEachKey(handler func(key string, payload *commonpb.Payload) error) error {
	for k, v := range m.fields {
		if err := handler(k, v); err != nil {
			return err
		}
	}
	return nil
}
