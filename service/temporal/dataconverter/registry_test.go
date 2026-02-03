package dataconverter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
)

// mockPayloadCodec implements converter.PayloadCodec for testing
type mockPayloadCodec struct {
	name string
}

func (m *mockPayloadCodec) Encode(payloads []*commonpb.Payload) ([]*commonpb.Payload, error) {
	return payloads, nil
}

func (m *mockPayloadCodec) Decode(payloads []*commonpb.Payload) ([]*commonpb.Payload, error) {
	return payloads, nil
}

func TestRegistry_NewRegistry(t *testing.T) {
	t.Run("create with default base converter", func(t *testing.T) {
		baseConverter := converter.GetDefaultDataConverter()
		registry := NewRegistry(baseConverter)

		assert.NotNil(t, registry)
		assert.NotNil(t, registry.base)
	})

	t.Run("create with nil base converter", func(t *testing.T) {
		registry := NewRegistry(nil)

		assert.NotNil(t, registry)
		assert.Nil(t, registry.base)
	})
}

func TestRegistry_RegisterCodec(t *testing.T) {
	t.Run("register single codec", func(t *testing.T) {
		baseConverter := converter.GetDefaultDataConverter()
		registry := NewRegistry(baseConverter)

		codec1 := &mockPayloadCodec{name: "codec1"}
		registry.RegisterCodec(codec1)

		assert.Len(t, registry.codecs, 1)
		assert.Equal(t, codec1, registry.codecs[0])
	})

	t.Run("register multiple codecs", func(t *testing.T) {
		baseConverter := converter.GetDefaultDataConverter()
		registry := NewRegistry(baseConverter)

		codec1 := &mockPayloadCodec{name: "codec1"}
		codec2 := &mockPayloadCodec{name: "codec2"}
		codec3 := &mockPayloadCodec{name: "codec3"}

		registry.RegisterCodec(codec1)
		registry.RegisterCodec(codec2)
		registry.RegisterCodec(codec3)

		assert.Len(t, registry.codecs, 3)
		assert.Equal(t, codec1, registry.codecs[0])
		assert.Equal(t, codec2, registry.codecs[1])
		assert.Equal(t, codec3, registry.codecs[2])
	})

	t.Run("register maintains order", func(t *testing.T) {
		baseConverter := converter.GetDefaultDataConverter()
		registry := NewRegistry(baseConverter)

		codecs := make([]*mockPayloadCodec, 10)
		for i := 0; i < 10; i++ {
			codecs[i] = &mockPayloadCodec{name: string(rune('A' + i))}
			registry.RegisterCodec(codecs[i])
		}

		assert.Len(t, registry.codecs, 10)
		for i, codec := range registry.codecs {
			assert.Equal(t, codecs[i], codec)
		}
	})
}

func TestRegistry_Build(t *testing.T) {
	t.Run("build with no codecs returns base converter", func(t *testing.T) {
		baseConverter := converter.GetDefaultDataConverter()
		registry := NewRegistry(baseConverter)

		result := registry.Build()

		assert.NotNil(t, result)
		assert.Equal(t, baseConverter, result)
	})

	t.Run("build with codecs returns codec data converter", func(t *testing.T) {
		baseConverter := converter.GetDefaultDataConverter()
		registry := NewRegistry(baseConverter)

		codec1 := &mockPayloadCodec{name: "codec1"}
		registry.RegisterCodec(codec1)

		result := registry.Build()

		assert.NotNil(t, result)
		assert.NotEqual(t, baseConverter, result, "should wrap base converter with codec")
	})

	t.Run("build with multiple codecs", func(t *testing.T) {
		baseConverter := converter.GetDefaultDataConverter()
		registry := NewRegistry(baseConverter)

		codec1 := &mockPayloadCodec{name: "codec1"}
		codec2 := &mockPayloadCodec{name: "codec2"}
		registry.RegisterCodec(codec1)
		registry.RegisterCodec(codec2)

		result := registry.Build()

		assert.NotNil(t, result)
		assert.NotEqual(t, baseConverter, result)
	})

	t.Run("build is idempotent", func(t *testing.T) {
		baseConverter := converter.GetDefaultDataConverter()
		registry := NewRegistry(baseConverter)

		codec1 := &mockPayloadCodec{name: "codec1"}
		registry.RegisterCodec(codec1)

		result1 := registry.Build()
		result2 := registry.Build()

		assert.NotNil(t, result1)
		assert.NotNil(t, result2)
	})

	t.Run("build after registering more codecs", func(t *testing.T) {
		baseConverter := converter.GetDefaultDataConverter()
		registry := NewRegistry(baseConverter)

		codec1 := &mockPayloadCodec{name: "codec1"}
		registry.RegisterCodec(codec1)
		result1 := registry.Build()

		codec2 := &mockPayloadCodec{name: "codec2"}
		registry.RegisterCodec(codec2)
		result2 := registry.Build()

		assert.NotEqual(t, result1, result2, "building after new codec should create new converter")
	})
}

func TestRegistry_BuildFunctional(t *testing.T) {
	t.Run("built converter can encode and decode", func(t *testing.T) {
		baseConverter := converter.GetDefaultDataConverter()
		registry := NewRegistry(baseConverter)

		dataConverter := registry.Build()

		testData := map[string]interface{}{
			"key": "value",
			"num": 42,
		}

		payload, err := dataConverter.ToPayload(testData)
		require.NoError(t, err)
		assert.NotNil(t, payload)

		var decoded map[string]interface{}
		err = dataConverter.FromPayload(payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, "value", decoded["key"])
		assert.Equal(t, float64(42), decoded["num"])
	})

	t.Run("built converter with codec can encode and decode", func(t *testing.T) {
		baseConverter := converter.GetDefaultDataConverter()
		registry := NewRegistry(baseConverter)

		codec := &mockPayloadCodec{name: "passthrough"}
		registry.RegisterCodec(codec)

		dataConverter := registry.Build()

		testData := "test string"

		payload, err := dataConverter.ToPayload(testData)
		require.NoError(t, err)
		assert.NotNil(t, payload)

		var decoded string
		err = dataConverter.FromPayload(payload, &decoded)
		require.NoError(t, err)
		assert.Equal(t, testData, decoded)
	})
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent register and build", func(t *testing.T) {
		baseConverter := converter.GetDefaultDataConverter()
		registry := NewRegistry(baseConverter)
		done := make(chan bool)

		go func() {
			for i := 0; i < 100; i++ {
				registry.RegisterCodec(&mockPayloadCodec{name: "writer"})
			}
			done <- true
		}()

		go func() {
			for i := 0; i < 100; i++ {
				_ = registry.Build()
			}
			done <- true
		}()

		<-done
		<-done

		assert.Len(t, registry.codecs, 100)
	})

	t.Run("multiple concurrent writers", func(t *testing.T) {
		baseConverter := converter.GetDefaultDataConverter()
		registry := NewRegistry(baseConverter)
		numWriters := 10
		writesPerWriter := 10
		done := make(chan bool, numWriters)

		for w := 0; w < numWriters; w++ {
			go func() {
				for i := 0; i < writesPerWriter; i++ {
					registry.RegisterCodec(&mockPayloadCodec{name: "writer"})
				}
				done <- true
			}()
		}

		for i := 0; i < numWriters; i++ {
			<-done
		}

		assert.Len(t, registry.codecs, numWriters*writesPerWriter)
	})
}

func TestRegistry_NilCodec(t *testing.T) {
	t.Run("register nil codec", func(t *testing.T) {
		baseConverter := converter.GetDefaultDataConverter()
		registry := NewRegistry(baseConverter)

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("should not panic when registering nil codec")
			}
		}()

		registry.RegisterCodec(nil)
		assert.Len(t, registry.codecs, 1)
		assert.Nil(t, registry.codecs[0])
	})
}
