package payload

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/wippyai/runtime/api/payload"
)

// MockFormatTranscoder is a mock implementation of FormatTranscoder for testing.
type MockFormatTranscoder struct {
	From payload.Format
	To   payload.Format
	Func func(payload.Payload) (payload.Payload, error)
}

func (m *MockFormatTranscoder) Transcode(p payload.Payload) (payload.Payload, error) {
	if m.Func != nil {
		return m.Func(p)
	}
	return payload.NewPayload(p.Data(), m.To), nil
}

// MockUnmarshaler is a mock implementation of Unmarshaler for testing.
type MockUnmarshaler struct {
	Format payload.Format
	Func   func(payload.Payload, interface{}) error
}

func (m *MockUnmarshaler) Unmarshal(p payload.Payload, v interface{}) error {
	if m.Func != nil {
		return m.Func(p, v)
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("invalid unmarshal target")
	}
	rv.Elem().Set(reflect.ValueOf(p.Data()))
	return nil
}

func TestTranscoder_RegisterTranscoderAndTranscode(t *testing.T) {
	// Spawn a local, isolated instance of the transcoder for testing
	transcoder := NewTranscoder()

	// Define some mock formats
	formatA := payload.Format("format/A")
	formatB := payload.Format("format/B")
	formatC := payload.Format("format/C")

	// Spawn mock json
	transcoderAB := &MockFormatTranscoder{
		From: formatA,
		To:   formatB,
		Func: func(p payload.Payload) (payload.Payload, error) {
			return payload.NewPayload(fmt.Sprintf("%s_AB", p.Data()), formatB), nil
		},
	}

	transcoderBC := &MockFormatTranscoder{
		From: formatB,
		To:   formatC,
		Func: func(p payload.Payload) (payload.Payload, error) {
			return payload.NewPayload(fmt.Sprintf("%s_BC", p.Data()), formatC), nil
		},
	}

	// Register the json using the provided methods
	transcoder.RegisterTranscoder(formatA, formatB, 1, transcoderAB)
	transcoder.RegisterTranscoder(formatB, formatC, 1, transcoderBC)

	// Spawn a payload
	p := payload.NewPayload("test", formatA)

	// Transcode the payload from A to C
	transcodedPayload, err := transcoder.Transcode(p, formatC)
	if err != nil {
		t.Fatalf("Transcode failed: %v", err)
	}

	// Verify the transcoded payload
	expectedData := "test_AB_BC"
	if transcodedPayload.Format() != formatC {
		t.Errorf("Expected format %s, got %s", formatC, transcodedPayload.Format())
	}
	if transcodedPayload.Data() != expectedData {
		t.Errorf("Expected data %s, got %s", expectedData, transcodedPayload.Data())
	}
}

func TestTranscoder_RegisterUnmarshalerAndUnmarshal(t *testing.T) {
	// Spawn a local, isolated instance of the transcoder for testing
	transcoder := NewTranscoder()

	// Define some mock formats
	formatA := payload.Format("format/A")
	formatB := payload.Format("format/B")

	// Spawn a mock unmarshaler
	unmarshalerB := &MockUnmarshaler{
		Format: formatB,
		Func: func(p payload.Payload, v interface{}) error {
			rv := reflect.ValueOf(v)
			if rv.Kind() != reflect.Ptr || rv.IsNil() {
				return fmt.Errorf("invalid unmarshal target")
			}
			rv.Elem().Set(reflect.ValueOf(fmt.Sprintf("%s_unmarshaled", p.Data())))
			return nil
		},
	}

	// Spawn mock json
	transcoderAB := &MockFormatTranscoder{
		From: formatA,
		To:   formatB,
		Func: func(p payload.Payload) (payload.Payload, error) {
			return payload.NewPayload(fmt.Sprintf("%s_AB", p.Data()), formatB), nil
		},
	}

	// Register the unmarshaler and transcoder using the provided methods
	transcoder.RegisterUnmarshaler(formatB, unmarshalerB)
	transcoder.RegisterTranscoder(formatA, formatB, 1, transcoderAB)

	// Spawn a payload
	p := payload.NewPayload("test", formatA)

	// Unmarshal the payload
	var result string
	err := transcoder.Unmarshal(p, &result)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify the unmarshaled result
	expectedResult := "test_AB_unmarshaled"
	if result != expectedResult {
		t.Errorf("Expected result %s, got %s", expectedResult, result)
	}
}

func TestTranscoder_NoTranscodingPath(t *testing.T) {
	// Spawn a local, isolated instance of the transcoder for testing
	transcoder := NewTranscoder()

	// Define some mock formats
	formatA := payload.Format("format/A")
	formatB := payload.Format("format/B")

	// DO NOT register any json. This ensures there's no path.

	// Spawn a payload
	p := payload.NewPayload("test", formatA)

	// Try to transcode to a format with no path
	_, err := transcoder.Transcode(p, formatB)
	if err == nil {
		t.Fatalf("Transcode should have failed")
	}

	expectedError := fmt.Sprintf("no transcoding path found from %s to %s", formatA, formatB)
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestTranscoder_NoUnmarshalingPath(t *testing.T) {
	// Spawn a local, isolated instance of the transcoder for testing
	transcoder := NewTranscoder()

	// Define some mock formats
	formatA := payload.Format("format/A")

	// DO NOT register any unmarshalers.

	// Spawn a payload
	p := payload.NewPayload("test", formatA)

	// Try to unmarshal a payload with no unmarshaling path
	var result string
	err := transcoder.Unmarshal(p, &result)
	if err == nil {
		t.Fatalf("Unmarshal should have failed")
	}

	expectedError := fmt.Sprintf("no unmarshaling path found for format %s", formatA)
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestTranscoder_ConcurrentAccess(t *testing.T) {
	transcoder := NewTranscoder()
	formatA := payload.Format("format/A")
	formatB := payload.Format("format/B")
	formatC := payload.Format("format/C")

	// Create mock transcoders
	transcoderAB := &MockFormatTranscoder{
		From: formatA,
		To:   formatB,
		Func: func(p payload.Payload) (payload.Payload, error) {
			return payload.NewPayload(fmt.Sprintf("%s_AB", p.Data()), formatB), nil
		},
	}

	transcoderBC := &MockFormatTranscoder{
		From: formatB,
		To:   formatC,
		Func: func(p payload.Payload) (payload.Payload, error) {
			return payload.NewPayload(fmt.Sprintf("%s_BC", p.Data()), formatC), nil
		},
	}

	// Register transcoders
	transcoder.RegisterTranscoder(formatA, formatB, 1, transcoderAB)
	transcoder.RegisterTranscoder(formatB, formatC, 1, transcoderBC)

	// Test concurrent transcoding
	const numGoroutines = 10
	done := make(chan struct{})

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()

			p := payload.NewPayload(fmt.Sprintf("test_%d", id), formatA)
			_, err := transcoder.Transcode(p, formatC)
			if err != nil {
				t.Errorf("Transcode failed in goroutine %d: %v", id, err)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

func TestTranscoder_TranscoderErrorHandling(t *testing.T) {
	transcoder := NewTranscoder()
	formatA := payload.Format("format/A")
	formatB := payload.Format("format/B")

	// Create a transcoder that returns an error
	errorTranscoder := &MockFormatTranscoder{
		From: formatA,
		To:   formatB,
		Func: func(_ payload.Payload) (payload.Payload, error) {
			return payload.NewPayload("", formatB), fmt.Errorf("transcoding error")
		},
	}

	transcoder.RegisterTranscoder(formatA, formatB, 1, errorTranscoder)

	// Attempt to transcode
	p := payload.NewPayload("test", formatA)
	_, err := transcoder.Transcode(p, formatB)
	if err == nil {
		t.Error("Expected error from transcoder, got nil")
	}
}

func TestTranscoder_UnmarshalerErrorHandling(t *testing.T) {
	transcoder := NewTranscoder()
	formatA := payload.Format("format/A")

	// Create an unmarshaler that returns an error
	errorUnmarshaler := &MockUnmarshaler{
		Format: formatA,
		Func: func(_ payload.Payload, _ interface{}) error {
			return fmt.Errorf("unmarshaling error")
		},
	}

	transcoder.RegisterUnmarshaler(formatA, errorUnmarshaler)

	// Attempt to unmarshal
	p := payload.NewPayload("test", formatA)
	var result string
	err := transcoder.Unmarshal(p, &result)
	if err == nil {
		t.Error("Expected error from unmarshaler, got nil")
	}
}

func TestTranscoder_InvalidUnmarshalTarget(t *testing.T) {
	transcoder := NewTranscoder()
	formatA := payload.Format("format/A")

	// Register an unmarshaler
	unmarshaler := &MockUnmarshaler{
		Format: formatA,
	}
	transcoder.RegisterUnmarshaler(formatA, unmarshaler)

	// Test with nil target
	p := payload.NewPayload("test", formatA)
	err := transcoder.Unmarshal(p, nil)
	if err == nil {
		t.Error("Expected error for nil target, got nil")
	}

	// Test with non-pointer target
	var result string
	err = transcoder.Unmarshal(p, result)
	if err == nil {
		t.Error("Expected error for non-pointer target, got nil")
	}
}

func TestTranscoder_SameFormat(t *testing.T) {
	transcoder := NewTranscoder()
	formatA := payload.Format("format/A")

	p := payload.NewPayload("test", formatA)
	result, err := transcoder.Transcode(p, formatA)
	if err != nil {
		t.Errorf("Transcode same format should not error: %v", err)
	}
	if result != p {
		t.Error("Transcode same format should return same payload")
	}
}

func TestTranscoder_EmptyPayloadFormat(t *testing.T) {
	transcoder := NewTranscoder()

	p := payload.NewPayload("test", "")
	var result string
	err := transcoder.Unmarshal(p, &result)
	if err == nil {
		t.Error("Expected error for empty format, got nil")
	}
}

func TestTranscoder_GlobalTranscoder(t *testing.T) {
	t1 := GlobalTranscoder()
	t2 := GlobalTranscoder()
	if t1 != t2 {
		t.Error("GlobalTranscoder should return same instance")
	}
}

// Benchmarks

func BenchmarkTranscode_SingleStep(b *testing.B) {
	transcoder := NewTranscoder()
	formatA := payload.Format("format/A")
	formatB := payload.Format("format/B")

	transcoderAB := &MockFormatTranscoder{
		From: formatA,
		To:   formatB,
	}
	transcoder.RegisterTranscoder(formatA, formatB, 1, transcoderAB)

	p := payload.NewPayload("test", formatA)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = transcoder.Transcode(p, formatB)
	}
}

func BenchmarkTranscode_MultiStep(b *testing.B) {
	transcoder := NewTranscoder()
	formatA := payload.Format("format/A")
	formatB := payload.Format("format/B")
	formatC := payload.Format("format/C")

	transcoder.RegisterTranscoder(formatA, formatB, 1, &MockFormatTranscoder{From: formatA, To: formatB})
	transcoder.RegisterTranscoder(formatB, formatC, 1, &MockFormatTranscoder{From: formatB, To: formatC})

	p := payload.NewPayload("test", formatA)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = transcoder.Transcode(p, formatC)
	}
}

func BenchmarkTranscode_SameFormat(b *testing.B) {
	transcoder := NewTranscoder()
	formatA := payload.Format("format/A")
	p := payload.NewPayload("test", formatA)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = transcoder.Transcode(p, formatA)
	}
}

func BenchmarkUnmarshal_Direct(b *testing.B) {
	transcoder := NewTranscoder()
	formatA := payload.Format("format/A")

	transcoder.RegisterUnmarshaler(formatA, &MockUnmarshaler{Format: formatA})

	p := payload.NewPayload("test", formatA)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result string
		_ = transcoder.Unmarshal(p, &result)
	}
}
