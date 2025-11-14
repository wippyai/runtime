package yaml

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"gopkg.in/yaml.v3"
)

func TestYamlToGolangTranscoder_Transcode(t *testing.T) {
	tests := []struct {
		name    string
		payload payload.Payload
		want    payload.Payload
		wantErr bool
	}{
		{
			name:    "Valid YAML string",
			payload: payload.NewPayload("key: value", payload.YAML),
			want:    payload.NewPayload(map[string]interface{}{"key": "value"}, payload.Golang),
			wantErr: false,
		},
		{
			name:    "Valid YAML bytes",
			payload: payload.NewPayload([]byte("key: value"), payload.YAML),
			want:    payload.NewPayload(map[string]interface{}{"key": "value"}, payload.Golang),
			wantErr: false,
		},
		{
			name:    "Invalid YAML",
			payload: payload.NewPayload("key: value\ninvalid", payload.YAML),
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
			name:    "Unsupported YAML data type",
			payload: payload.NewPayload(123, payload.YAML),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Invalid YAML bytes",
			payload: payload.NewPayload([]byte("key: value\ninvalid"), payload.YAML),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Complex nested YAML",
			payload: payload.NewPayload("parent:\n  child:\n    - item1\n    - item2\n  value: 42", payload.YAML),
			want: payload.NewPayload(map[string]interface{}{
				"parent": map[string]interface{}{
					"child": []interface{}{"item1", "item2"},
					"value": 42,
				},
			}, payload.Golang),
			wantErr: false,
		},
		{
			name:    "Empty YAML",
			payload: payload.NewPayload("", payload.YAML),
			want:    payload.NewPayload(nil, payload.Golang),
			wantErr: false,
		},
		{
			name:    "YAML with null value",
			payload: payload.NewPayload("key: null", payload.YAML),
			want:    payload.NewPayload(map[string]interface{}{"key": nil}, payload.Golang),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &ToGolang{}
			got, err := tr.Transcode(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("YamlToGolangTranscoder.Transcode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && !reflect.DeepEqual(got.Data(), tt.want.Data()) {
				t.Errorf("YamlToGolangTranscoder.Transcode() = %v, want %v", got.Data(), tt.want.Data())
			}
			if !tt.wantErr && got.Format() != tt.want.Format() {
				t.Errorf("YamlToGolangTranscoder.Transcode() format = %v, want %v", got.Format(), tt.want.Format())
			}
		})
	}
}

func TestYamlToGolangTranscoder_Unmarshal(t *testing.T) {
	type TestStruct struct {
		Key string `yaml:"key"`
	}

	type ComplexStruct struct {
		Parent struct {
			Child []string `yaml:"child"`
			Value int      `yaml:"value"`
		} `yaml:"parent"`
	}

	tests := []struct {
		name    string
		payload payload.Payload
		target  interface{}
		want    interface{}
		wantErr bool
	}{
		{
			name:    "Valid YAML string to struct",
			payload: payload.NewPayload("key: value", payload.YAML),
			target:  &TestStruct{},
			want:    &TestStruct{Key: "value"},
			wantErr: false,
		},
		{
			name:    "Valid YAML bytes to struct",
			payload: payload.NewPayload([]byte("key: value"), payload.YAML),
			target:  &TestStruct{},
			want:    &TestStruct{Key: "value"},
			wantErr: false,
		},
		{
			name:    "Invalid YAML",
			payload: payload.NewPayload("key: value\ninvalid", payload.YAML),
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
			payload: payload.NewPayload("key: value", payload.YAML),
			target:  "not a pointer",
			want:    "",
			wantErr: true,
		},
		{
			name:    "Unsupported unmarshal data type",
			payload: payload.NewPayload(123, payload.YAML),
			target:  &TestStruct{},
			want:    &TestStruct{},
			wantErr: true,
		},
		{
			name:    "Complex nested YAML to struct",
			payload: payload.NewPayload("parent:\n  child:\n    - item1\n    - item2\n  value: 42", payload.YAML),
			target:  &ComplexStruct{},
			want: &ComplexStruct{
				Parent: struct {
					Child []string `yaml:"child"`
					Value int      `yaml:"value"`
				}{
					Child: []string{"item1", "item2"},
					Value: 42,
				},
			},
			wantErr: false,
		},
		{
			name:    "Empty YAML to struct",
			payload: payload.NewPayload("", payload.YAML),
			target:  &TestStruct{},
			want:    &TestStruct{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := &ToGolang{}
			err := tr.Unmarshal(tt.payload, tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("YamlToGolangTranscoder.Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(tt.target, tt.want) {
				t.Errorf("YamlToGolangTranscoder.Unmarshal() = %v, want %v", tt.target, tt.want)
			}
		})
	}
}

func TestGolangToYamlTranscoder_Transcode(t *testing.T) {
	tests := []struct {
		name    string
		payload payload.Payload
		want    payload.Payload
		wantErr bool
	}{
		{
			name:    "Valid struct",
			payload: payload.NewPayload(struct{ Key string }{Key: "value"}, payload.Golang),
			want:    payload.NewPayload("key: value\n", payload.YAML),
			wantErr: false,
		},
		{
			name:    "Valid map",
			payload: payload.NewPayload(map[string]string{"key": "value"}, payload.Golang),
			want:    payload.NewPayload("key: value\n", payload.YAML),
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
		{
			name: "Complex nested structure",
			payload: payload.NewPayload(map[string]interface{}{
				"parent": map[string]interface{}{
					"child": []interface{}{"item1", "item2"},
					"value": 42,
				},
			}, payload.Golang),
			want:    payload.NewPayload("parent:\n  child:\n    - item1\n    - item2\n  value: 42\n", payload.YAML),
			wantErr: false,
		},
		{
			name:    "Nil value",
			payload: payload.NewPayload(nil, payload.Golang),
			want:    payload.NewPayload("null\n", payload.YAML),
			wantErr: false,
		},
		{
			name:    "Empty map",
			payload: payload.NewPayload(map[string]interface{}{}, payload.Golang),
			want:    payload.NewPayload("{}\n", payload.YAML),
			wantErr: false,
		},
		{
			name:    "Empty slice",
			payload: payload.NewPayload([]interface{}{}, payload.Golang),
			want:    payload.NewPayload("[]\n", payload.YAML),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Recover from panic
			defer func() {
				if r := recover(); r != nil {
					if !tt.wantErr {
						t.Errorf("GolangToYamlTranscoder.Transcode() unexpected panic: %v", r)
					}
				}
			}()

			tr := &FromGolang{}
			got, err := tr.Transcode(tt.payload)

			if (err != nil) != tt.wantErr {
				t.Errorf("GolangToYamlTranscoder.Transcode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got.Format() != tt.want.Format() {
				t.Errorf("GolangToYamlTranscoder.Transcode() format = %v, want %v", got.Format(), tt.want.Format())
			}
			// For comparing YAML strings, unmarshal them first
			if !tt.wantErr {
				var gotData, wantData interface{}
				_ = yaml.Unmarshal([]byte(got.Data().(string)), &gotData)
				_ = yaml.Unmarshal([]byte(tt.want.Data().(string)), &wantData)
				if !reflect.DeepEqual(gotData, wantData) {
					t.Errorf("GolangToYamlTranscoder.Transcode() data = %v, want %v", gotData, wantData)
				}
			}
		})
	}
}

func TestRegister(t *testing.T) {
	// Create a mock transcoder register
	mockRegister := &mockTranscoderRegister{
		registeredTranscoders:  make(map[string]payload.FormatTranscoder),
		registeredUnmarshalers: make(map[payload.Format]payload.Unmarshaler),
	}

	// Register the YAML transcoders
	Register(mockRegister)

	// Verify that both transcoders were registered
	if len(mockRegister.registeredTranscoders) != 2 {
		t.Errorf("Expected 2 registered transcoders, got %d", len(mockRegister.registeredTranscoders))
	}

	// Verify that the unmarshaler was registered
	if len(mockRegister.registeredUnmarshalers) != 1 {
		t.Errorf("Expected 1 registered unmarshaler, got %d", len(mockRegister.registeredUnmarshalers))
	}

	// Verify that the unmarshaler is for YAML format
	if _, ok := mockRegister.registeredUnmarshalers[payload.YAML]; !ok {
		t.Error("Expected YAML unmarshaler to be registered")
	}
}

// mockTranscoderRegister implements payload.TranscoderRegister for testing
type mockTranscoderRegister struct {
	registeredTranscoders  map[string]payload.FormatTranscoder
	registeredUnmarshalers map[payload.Format]payload.Unmarshaler
}

func (m *mockTranscoderRegister) RegisterTranscoder(from, to payload.Format, _ int, transcoder payload.FormatTranscoder) {
	key := fmt.Sprintf("%s->%s", from, to)
	m.registeredTranscoders[key] = transcoder
}

func (m *mockTranscoderRegister) RegisterUnmarshaler(format payload.Format, unmarshaler payload.Unmarshaler) {
	m.registeredUnmarshalers[format] = unmarshaler
}
