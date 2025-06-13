// file: internode/codec_test.go
package internode

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	luacore "github.com/yuin/gopher-lua"
)

// fakeTranscoder is a mock implementation of payload.Transcoder for testing.
// It only knows how to convert a Lua payload to a JSON payload.
type fakeTranscoder struct{}

// Transcode implements the payload.Transcoder interface.
func (ft *fakeTranscoder) Transcode(p payload.Payload, to payload.Format) (payload.Payload, error) {
	// We only mock the specific path needed for our test.
	if p.Format() == payload.Lua && to == payload.JSON {
		luaTable, ok := p.Data().(*luacore.LTable)
		if !ok {
			return nil, fmt.Errorf("fake transcoder expected *LTable for Lua payload")
		}

		// Convert LTable to a Go map to make it JSON-serializable.
		goMap := make(map[string]interface{})
		luaTable.ForEach(func(key luacore.LValue, value luacore.LValue) {
			goMap[key.String()] = value.String()
		})

		jsonBytes, err := json.Marshal(goMap)
		if err != nil {
			return nil, err
		}
		// Return a new payload with the JSON string data and JSON format.
		return payload.NewPayload(string(jsonBytes), payload.JSON), nil
	}
	return nil, fmt.Errorf("transcode path from %s to %s is not supported by fakeTranscoder", p.Format(), to)
}

// Unmarshal implements the payload.Unmarshaler interface. Not needed for this test.
func (ft *fakeTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	return fmt.Errorf("unmarshal is not implemented in fakeTranscoder")
}

// assertPackagesEqual is a helper to robustly compare two packages.
func assertPackagesEqual(t *testing.T, expected, actual *pubsub.Package) {
	t.Helper()
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Decoded package does not match original.\nOriginal: %+v\nDecoded:  %+v", expected, actual)
	}
}

type CustomData struct{ Name string }

// TestMessageCodec_Roundtrip_GoPayload tests a simple case with a native Go struct.
// The transcoder should not be invoked for this payload.
func TestMessageCodec_Roundtrip_GoPayload(t *testing.T) {
	transcoder := &fakeTranscoder{}
	codec := NewMessageCodec(transcoder)
	gob.Register(CustomData{}) // Test-specific type registration

	originalPkg := &pubsub.Package{
		Messages: []*pubsub.Message{{
			Payloads: []payload.Payload{
				payload.NewPayload(CustomData{Name: "Test"}, payload.Golang),
			},
		}},
	}

	encoded, err := codec.Encode(originalPkg)
	if err != nil {
		t.Fatalf("Encode() failed with error: %v", err)
	}
	decodedPkg, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() failed with error: %v", err)
	}
	assertPackagesEqual(t, originalPkg, decodedPkg)
}

// TestMessageCodec_Roundtrip_LuaPayload_Normalized tests that a complex type (Lua table)
// is correctly normalized to JSON by the transcoder during the encoding process.
func TestMessageCodec_Roundtrip_LuaPayload_Normalized(t *testing.T) {
	transcoder := &fakeTranscoder{}
	codec := NewMessageCodec(transcoder)

	luaState := luacore.NewState()
	defer luaState.Close()
	luaTable := luaState.NewTable()
	luaTable.RawSetString("msg", luacore.LString("from lua"))

	originalPkg := &pubsub.Package{
		Messages: []*pubsub.Message{{Payloads: []payload.Payload{payload.NewPayload(luaTable, payload.Lua)}}},
	}

	encoded, err := codec.Encode(originalPkg)
	if err != nil {
		t.Fatalf("Encode() failed: %v", err)
	}
	decodedPkg, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() failed: %v", err)
	}

	// ASSERTIONS: Check that the payload was correctly normalized.
	if len(decodedPkg.Messages) != 1 || len(decodedPkg.Messages[0].Payloads) != 1 {
		t.Fatal("Decoded package has incorrect structure")
	}

	normalizedPayload := decodedPkg.Messages[0].Payloads[0]
	if normalizedPayload.Format() != payload.JSON {
		t.Fatalf("Expected payload format to be JSON, got %s", normalizedPayload.Format())
	}

	jsonData, ok := normalizedPayload.Data().(string)
	if !ok {
		t.Fatalf("Expected normalized payload data to be a string, got %T", normalizedPayload.Data())
	}

	if !strings.Contains(jsonData, `"msg":"from lua"`) {
		t.Fatalf(`Expected JSON data to contain '"msg":"from lua"', got %s`, jsonData)
	}
}

// TestMessageCodec_UnregisteredType verifies that encoding a Golang payload
// with an unregistered type still fails as expected.
func TestMessageCodec_UnregisteredType(t *testing.T) {
	transcoder := &fakeTranscoder{}
	codec := NewMessageCodec(transcoder)
	type UnregisteredStruct struct{ ID string }
	pkg := &pubsub.Package{
		Messages: []*pubsub.Message{
			{Payloads: []payload.Payload{payload.NewPayload(UnregisteredStruct{ID: "test"}, payload.Golang)}},
		},
	}
	_, err := codec.Encode(pkg)
	if err == nil {
		t.Fatal("Encode should fail for unregistered types, but it didn't")
	}

	expectedErr := "type not registered" // Check for gob's specific error text
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("Expected error message to contain %q, but got: %v", expectedErr, err)
	}
}
