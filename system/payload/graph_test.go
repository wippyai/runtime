package payload_test

import (
	"reflect"
	"testing"

	apipayload "github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"github.com/wippyai/runtime/system/payload/yaml"
)

func TestTranscoder_Unmarshal_Simple(t *testing.T) {
	transcoder := payload.NewTranscoder()
	json.Register(transcoder)
	yaml.Register(transcoder)

	tests := []struct {
		inputPayload  apipayload.Payload
		targetType    interface{}
		expectedValue interface{}
		name          string
		expectError   bool
	}{
		{
			name:          "Unmarshal JSON to map",
			inputPayload:  apipayload.NewPayload(`{"key": "value"}`, apipayload.JSON),
			targetType:    &map[string]interface{}{},
			expectedValue: map[string]interface{}{"key": "value"},
			expectError:   false,
		},
		{
			name:          "Unmarshal YAML to map",
			inputPayload:  apipayload.NewPayload("key: value", apipayload.YAML),
			targetType:    &map[string]interface{}{},
			expectedValue: map[string]interface{}{"key": "value"},
			expectError:   false,
		},
		{
			name: "Unmarshal JSON to struct",
			inputPayload: apipayload.NewPayload(`{
				"Name": "John Doe",
				"Age": 30,
				"Address": {
					"Street": "123 Main St",
					"City": "Anytown"
				}
			}`, apipayload.JSON),
			targetType: &struct {
				Address struct {
					Street string
					City   string
				}
				Name string
				Age  int
			}{},
			expectedValue: struct {
				Address struct {
					Street string
					City   string
				}
				Name string
				Age  int
			}{
				Name: "John Doe",
				Age:  30,
				Address: struct {
					Street string
					City   string
				}{
					Street: "123 Main St",
					City:   "Anytown",
				},
			},
			expectError: false,
		},
		{
			name: "Unmarshal YAML to struct",
			inputPayload: apipayload.NewPayload(`
name: "John Doe"
age: 30
address:
  street: 123 Main St
  city: Anytown
`, apipayload.YAML),
			targetType: &struct {
				Address struct {
					Street string
					City   string
				}
				Name string
				Age  int
			}{},
			expectedValue: struct {
				Address struct {
					Street string
					City   string
				}
				Name string
				Age  int
			}{
				Name: "John Doe",
				Age:  30,
				Address: struct {
					Street string
					City   string
				}{
					Street: "123 Main St",
					City:   "Anytown",
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targetValue := reflect.New(reflect.TypeOf(tt.targetType).Elem()).Interface()
			err := transcoder.Unmarshal(tt.inputPayload, targetValue)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected an error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				if !reflect.DeepEqual(reflect.ValueOf(targetValue).Elem().Interface(), tt.expectedValue) {
					t.Errorf("unmarshaled value does not match expected value\ngot:  %v\nwant: %v", reflect.ValueOf(targetValue).Elem().Interface(), tt.expectedValue)
				}
			}
		})
	}
}

func TestTranscoder_Transcode_MultiStep(t *testing.T) {
	transcoder := payload.NewTranscoder()
	json.Register(transcoder)
	yaml.Register(transcoder)

	tests := []struct {
		inputPayload    apipayload.Payload
		expectedPayload apipayload.Payload
		name            string
		targetFormat    apipayload.Format
		expectError     bool
	}{
		{
			name:            "Transcode YAML to JSON",
			inputPayload:    apipayload.NewPayload("key: value", apipayload.YAML),
			targetFormat:    apipayload.JSON,
			expectedPayload: apipayload.NewPayload(`{"key":"value"}`, apipayload.JSON),
			expectError:     false,
		},
		{
			name: "Transcode YAML to JSON (complex)",
			inputPayload: apipayload.NewPayload(`
person:
  name: John Doe
  age: 30
  address:
    street: 123 Main St
    city: Anytown
`, apipayload.YAML),
			targetFormat:    apipayload.JSON,
			expectedPayload: apipayload.NewPayload(`{"person":{"address":{"city":"Anytown","street":"123 Main St"},"age":30,"name":"John Doe"}}`, apipayload.JSON),
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transcodedPayload, err := transcoder.Transcode(tt.inputPayload, tt.targetFormat)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected an error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				if transcodedPayload.Format() != tt.expectedPayload.Format() {
					t.Errorf("transcoded payload format does not match expected format\ngot:  %v\nwant: %v", transcodedPayload.Format(), tt.expectedPayload.Format())
				}
				switch tt.expectedPayload.Format() {
				case apipayload.JSON:
					var got, want interface{}
					jt := &json.ToGolang{}

					if err := jt.Unmarshal(transcodedPayload, &got); err != nil {
						t.Errorf("failed to unmarshal transcoded payload from json: %v", err)
					}
					if err := jt.Unmarshal(tt.expectedPayload, &want); err != nil {
						t.Errorf("failed to unmarshal expected payload from json: %v", err)
					}

					if !reflect.DeepEqual(got, want) {
						t.Errorf("transcoded payload data does not match expected data\ngot:  %v\nwant: %v", got, want)
					}
				case apipayload.YAML, apipayload.Golang, apipayload.Lua, apipayload.String, apipayload.Bytes, apipayload.GoError:
					fallthrough
				default:
					if !reflect.DeepEqual(transcodedPayload.Data(), tt.expectedPayload.Data()) {
						t.Errorf("transcoded payload data does not match expected data\ngot:  %v\nwant: %v", transcodedPayload.Data(), tt.expectedPayload.Data())
					}
				}
			}
		})
	}
}

func TestTranscoder_Unmarshal_AnyToStruct(t *testing.T) {
	transcoder := payload.NewTranscoder()
	json.Register(transcoder)
	yaml.Register(transcoder)

	tests := []struct {
		inputPayload  apipayload.Payload
		targetType    interface{}
		expectedValue interface{}
		name          string
		expectError   bool
	}{
		{
			name: "Unmarshal Golang ANY (from JSON) to struct",
			inputPayload: func() apipayload.Payload {
				// Spawn a JSON payload and then transcode it to Golang ANY
				jsonPayload := apipayload.NewPayload(`{
					"Name": "Jane Doe",
					"Age": 25,
					"Address": {
						"Street": "456 Oak Ave",
						"City": "Springfield"
					}
				}`, apipayload.JSON)
				golangAnyPayload, err := transcoder.Transcode(jsonPayload, apipayload.Golang)
				if err != nil {
					t.Fatalf("failed to transcode JSON to Golang ANY: %v", err)
				}
				return golangAnyPayload
			}(),
			targetType: &struct {
				Address struct {
					Street string
					City   string
				}
				Name string
				Age  int
			}{},
			expectedValue: struct {
				Address struct {
					Street string
					City   string
				}
				Name string
				Age  int
			}{
				Name: "Jane Doe",
				Age:  25,
				Address: struct {
					Street string
					City   string
				}{
					Street: "456 Oak Ave",
					City:   "Springfield",
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targetValue := reflect.New(reflect.TypeOf(tt.targetType).Elem()).Interface()
			err := transcoder.Unmarshal(tt.inputPayload, targetValue)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected an error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				if !reflect.DeepEqual(reflect.ValueOf(targetValue).Elem().Interface(), tt.expectedValue) {
					t.Errorf("unmarshaled value does not match expected value\ngot:  %v\nwant: %v", reflect.ValueOf(targetValue).Elem().Interface(), tt.expectedValue)
				}
			}
		})
	}
}

func TestTranscoder_Transcode_JSONToYAML(t *testing.T) {
	transcoder := payload.NewTranscoder()
	json.Register(transcoder)
	yaml.Register(transcoder)

	tests := []struct {
		inputPayload    apipayload.Payload
		expectedPayload apipayload.Payload
		name            string
		targetFormat    apipayload.Format
		expectError     bool
	}{
		{
			name:            "Transcode JSON to YAML",
			inputPayload:    apipayload.NewPayload(`{"key":"value"}`, apipayload.JSON),
			targetFormat:    apipayload.YAML,
			expectedPayload: apipayload.NewPayload("key: value\n", apipayload.YAML),
			expectError:     false,
		},
		{
			name:         "Transcode JSON to YAML (complex)",
			inputPayload: apipayload.NewPayload(`{"person":{"address":{"city":"Anytown","street":"123 Main St"},"age":30,"name":"John Doe"}}`, apipayload.JSON),
			targetFormat: apipayload.YAML,
			expectedPayload: apipayload.NewPayload(`person:
    address:
        city: Anytown
        street: 123 Main St
    age: 30
    name: John Doe
`, apipayload.YAML),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transcodedPayload, err := transcoder.Transcode(tt.inputPayload, tt.targetFormat)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected an error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				if transcodedPayload.Format() != tt.expectedPayload.Format() {
					t.Errorf("transcoded payload format does not match expected format\ngot:  %v\nwant: %v", transcodedPayload.Format(), tt.expectedPayload.Format())
				}

				if !reflect.DeepEqual(transcodedPayload.Data(), tt.expectedPayload.Data()) {
					t.Errorf("transcoded payload data does not match expected data\ngot:  %v\nwant: %v", transcodedPayload.Data(), tt.expectedPayload.Data())
				}
			}
		})
	}
}
