package payload

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
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
	// Create a local, isolated instance of the transcoder for testing
	transcoder := NewTranscoder()

	// Define some mock formats
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

	// Register the transcoders using the provided methods
	transcoder.RegisterTranscoder(formatA, formatB, 1, transcoderAB)
	transcoder.RegisterTranscoder(formatB, formatC, 1, transcoderBC)

	// Create a payload
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
	// Create a local, isolated instance of the transcoder for testing
	transcoder := NewTranscoder()

	// Define some mock formats
	formatA := payload.Format("format/A")
	formatB := payload.Format("format/B")

	// Create a mock unmarshaler
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

	// Create mock transcoders
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

	// Create a payload
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
	// Create a local, isolated instance of the transcoder for testing
	transcoder := NewTranscoder()

	// Define some mock formats
	formatA := payload.Format("format/A")
	formatB := payload.Format("format/B")

	// DO NOT register any transcoders. This ensures there's no path.

	// Create a payload
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
	// Create a local, isolated instance of the transcoder for testing
	transcoder := NewTranscoder()

	// Define some mock formats
	formatA := payload.Format("format/A")

	// DO NOT register any unmarshalers.

	// Create a payload
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
