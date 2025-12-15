package json

import (
	"reflect"
	"testing"

	"github.com/wippyai/runtime/api/payload"
)

func TestJsonToGolangTranscoder_Transcode(t *testing.T) {
	tests := []struct {
		name    string
		payload payload.Payload
		want    payload.Payload
		wantErr bool
	}{
		{
			name:    "Valid JSON string",
			payload: payload.NewPayload(`{"key": "value"}`, payload.JSON),
			want:    payload.NewPayload(map[string]interface{}{"key": "value"}, payload.Golang),
			wantErr: false,
		},
		{
			name:    "Valid JSON bytes",
			payload: payload.NewPayload([]byte(`{"key": "value"}`), payload.JSON),
			want:    payload.NewPayload(map[string]interface{}{"key": "value"}, payload.Golang),
			wantErr: false,
		},
		{
			name:    "Invalid JSON",
			payload: payload.NewPayload(`{"key": "value"`, payload.JSON),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Wrong input format",
			payload: payload.NewPayload("some string", payload.String),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Unsupported JSON data type",
			payload: payload.NewPayload(123, payload.JSON), // JSON number is not handled
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Invalid JSON bytes",
			payload: payload.NewPayload([]byte(`{"key": "value"`), payload.JSON),
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &ToGolang{}
			got, err := tr.Transcode(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("JsonToGolangTranscoder.Transcode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			// Deep equal comparison for the Data() part, since it can be a complex object
			if !tt.wantErr && !reflect.DeepEqual(got.Data(), tt.want.Data()) {
				t.Errorf("JsonToGolangTranscoder.Transcode() = %v, want %v", got.Data(), tt.want.Data())
			}
			if !tt.wantErr && got.Format() != tt.want.Format() {
				t.Errorf("JsonToGolangTranscoder.Transcode() format = %v, want %v", got.Format(), tt.want.Format())
			}
		})
	}
}

func TestJsonToGolangTranscoder_Unmarshal(t *testing.T) {
	type TestStruct struct {
		Key string `json:"key"`
	}

	tests := []struct {
		name    string
		payload payload.Payload
		target  interface{}
		want    interface{}
		wantErr bool
	}{
		{
			name:    "Valid JSON string to struct",
			payload: payload.NewPayload(`{"key": "value"}`, payload.JSON),
			target:  &TestStruct{},
			want:    &TestStruct{Key: "value"},
			wantErr: false,
		},
		{
			name:    "Valid JSON bytes to struct",
			payload: payload.NewPayload([]byte(`{"key": "value"}`), payload.JSON),
			target:  &TestStruct{},
			want:    &TestStruct{Key: "value"},
			wantErr: false,
		},
		{
			name:    "Invalid JSON",
			payload: payload.NewPayload(`{"key": "value"`, payload.JSON),
			target:  &TestStruct{},
			want:    &TestStruct{},
			wantErr: true,
		},
		{
			name:    "Wrong input format",
			payload: payload.NewPayload("some string", payload.String),
			target:  &TestStruct{},
			want:    &TestStruct{},
			wantErr: true,
		},
		{
			name:    "Unmarshal into wrong type",
			payload: payload.NewPayload(`{"key": "value"}`, payload.JSON),
			target:  "not a pointer",
			want:    "",
			wantErr: true,
		},
		{
			name:    "Unsupported unmarshal data type",
			payload: payload.NewPayload(123, payload.JSON),
			target:  &TestStruct{},
			want:    &TestStruct{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &ToGolang{}
			err := tr.Unmarshal(tt.payload, tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("JsonToGolangTranscoder.Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(tt.target, tt.want) {
				t.Errorf("JsonToGolangTranscoder.Unmarshal() = %v, want %v", tt.target, tt.want)
			}
		})
	}
}

func TestGolangToJsonTranscoder_Transcode(t *testing.T) {
	tests := []struct {
		name    string
		payload payload.Payload
		want    payload.Payload
		wantErr bool
	}{
		{
			name:    "Valid struct",
			payload: payload.NewPayload(struct{ Key string }{Key: "value"}, payload.Golang),
			want:    payload.NewPayload([]byte(`{"Key":"value"}`), payload.JSON),
			wantErr: false,
		},
		{
			name:    "Valid map",
			payload: payload.NewPayload(map[string]string{"key": "value"}, payload.Golang),
			want:    payload.NewPayload([]byte(`{"key":"value"}`), payload.JSON),
			wantErr: false,
		},
		{
			name:    "Invalid input format",
			payload: payload.NewPayload("some string", payload.String),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Marshal error",
			payload: payload.NewPayload(make(chan int), payload.Golang), // Channels cannot be marshaled
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &FromGolang{}
			got, err := tr.Transcode(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("GolangToJsonTranscoder.Transcode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got.Format() != tt.want.Format() {
				t.Errorf("GolangToJsonTranscoder.Transcode() format = %v, want %v", got.Format(), tt.want.Format())
			}
			if !tt.wantErr && !reflect.DeepEqual(got.Data(), tt.want.Data()) {
				t.Errorf("GolangToJsonTranscoder.Transcode() data = %v, want %v", got.Data(), tt.want.Data())
			}
		})
	}
}

func TestRegister(t *testing.T) {
	// Create a mock transcoder register
	mockRegister := &mockTranscoderRegister{
		transcoders:  make(map[string]interface{}),
		unmarshalers: make(map[string]interface{}),
	}

	// Register the JSON transcoders
	Register(mockRegister)

	// Debug: Print what was actually registered
	t.Logf("Registered transcoders: %v", mockRegister.transcoders)
	t.Logf("Registered unmarshalers: %v", mockRegister.unmarshalers)

	// Verify that the correct transcoders were registered
	if _, ok := mockRegister.transcoders["json/plain=>golang/any"]; !ok {
		t.Error("json/plain=>golang/any transcoder was not registered")
	}
	if _, ok := mockRegister.transcoders["golang/any=>json/plain"]; !ok {
		t.Error("golang/any=>json/plain transcoder was not registered")
	}
	if _, ok := mockRegister.unmarshalers["json/plain"]; !ok {
		t.Error("json/plain unmarshaler was not registered")
	}
}

// Mock implementation of TranscoderRegister for testing
type mockTranscoderRegister struct {
	transcoders  map[string]interface{}
	unmarshalers map[string]interface{}
}

func (m *mockTranscoderRegister) RegisterTranscoder(from, to payload.Format, _ int, transcoder payload.FormatTranscoder) {
	key := from + "=>" + to
	m.transcoders[key] = transcoder
}

func (m *mockTranscoderRegister) RegisterUnmarshaler(format payload.Format, unmarshaler payload.Unmarshaler) {
	m.unmarshalers[format] = unmarshaler
}

func TestJsonToGolangTranscoder_Transcode_Complex(t *testing.T) {
	tests := []struct {
		name    string
		payload payload.Payload
		want    payload.Payload
		wantErr bool
	}{
		{
			name:    "Nested JSON object",
			payload: payload.NewPayload(`{"outer": {"inner": {"key": "value"}}}`, payload.JSON),
			want:    payload.NewPayload(map[string]interface{}{"outer": map[string]interface{}{"inner": map[string]interface{}{"key": "value"}}}, payload.Golang),
			wantErr: false,
		},
		{
			name:    "JSON array",
			payload: payload.NewPayload(`[1, 2, 3, "four", true, null]`, payload.JSON),
			want:    payload.NewPayload([]interface{}{float64(1), float64(2), float64(3), "four", true, nil}, payload.Golang),
			wantErr: false,
		},
		{
			name:    "JSON with special values",
			payload: payload.NewPayload(`{"null": null, "number": 42.5, "boolean": true, "string": "text"}`, payload.JSON),
			want:    payload.NewPayload(map[string]interface{}{"null": nil, "number": 42.5, "boolean": true, "string": "text"}, payload.Golang),
			wantErr: false,
		},
		{
			name:    "Empty JSON object",
			payload: payload.NewPayload(`{}`, payload.JSON),
			want:    payload.NewPayload(map[string]interface{}{}, payload.Golang),
			wantErr: false,
		},
		{
			name:    "Empty JSON array",
			payload: payload.NewPayload(`[]`, payload.JSON),
			want:    payload.NewPayload([]interface{}{}, payload.Golang),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &ToGolang{}
			got, err := tr.Transcode(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("JsonToGolangTranscoder.Transcode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got.Data(), tt.want.Data()) {
				t.Errorf("JsonToGolangTranscoder.Transcode() = %v, want %v", got.Data(), tt.want.Data())
			}
			if !tt.wantErr && got.Format() != tt.want.Format() {
				t.Errorf("JsonToGolangTranscoder.Transcode() format = %v, want %v", got.Format(), tt.want.Format())
			}
		})
	}
}

func TestGolangToJsonTranscoder_Transcode_Complex(t *testing.T) {
	tests := []struct {
		name    string
		payload payload.Payload
		want    payload.Payload
		wantErr bool
	}{
		{
			name: "Nested struct",
			payload: payload.NewPayload(struct {
				Outer struct {
					Inner struct {
						Key string `json:"key"`
					} `json:"inner"`
				} `json:"outer"`
			}{
				Outer: struct {
					Inner struct {
						Key string `json:"key"`
					} `json:"inner"`
				}{
					Inner: struct {
						Key string `json:"key"`
					}{
						Key: "value",
					},
				},
			}, payload.Golang),
			want:    payload.NewPayload([]byte(`{"outer":{"inner":{"key":"value"}}}`), payload.JSON),
			wantErr: false,
		},
		{
			name:    "Slice with mixed types",
			payload: payload.NewPayload([]interface{}{1, 2.5, "three", true, nil}, payload.Golang),
			want:    payload.NewPayload([]byte(`[1,2.5,"three",true,null]`), payload.JSON),
			wantErr: false,
		},
		{
			name: "Map with special values",
			payload: payload.NewPayload(map[string]interface{}{
				"null":   nil,
				"number": 42.5,
				"bool":   true,
				"string": "text",
			}, payload.Golang),
			want:    payload.NewPayload([]byte(`{"bool":true,"null":null,"number":42.5,"string":"text"}`), payload.JSON),
			wantErr: false,
		},
		{
			name:    "Empty map",
			payload: payload.NewPayload(map[string]interface{}{}, payload.Golang),
			want:    payload.NewPayload([]byte(`{}`), payload.JSON),
			wantErr: false,
		},
		{
			name:    "Empty slice",
			payload: payload.NewPayload([]interface{}{}, payload.Golang),
			want:    payload.NewPayload([]byte(`[]`), payload.JSON),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &FromGolang{}
			got, err := tr.Transcode(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("GolangToJsonTranscoder.Transcode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got.Format() != tt.want.Format() {
				t.Errorf("GolangToJsonTranscoder.Transcode() format = %v, want %v", got.Format(), tt.want.Format())
			}
			if !tt.wantErr && !reflect.DeepEqual(got.Data(), tt.want.Data()) {
				t.Errorf("GolangToJsonTranscoder.Transcode() data = %v, want %v", got.Data(), tt.want.Data())
			}
		})
	}
}
