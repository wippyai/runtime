package payload

import (
	"reflect"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"github.com/wippyai/runtime/system/payload/yaml"
)

func TestTranscoder_Unmarshal_Simple(t *testing.T) {
	transcoder := NewTranscoder()
	json.Register(transcoder)
	yaml.Register(transcoder)

	tests := []struct {
		name          string
		inputPayload  payload.Payload
		targetType    interface{}
		expectedValue interface{}
		expectError   bool
	}{
		{
			name:          "Unmarshal JSON to map",
			inputPayload:  payload.NewPayload(`{"key": "value"}`, payload.JSON),
			targetType:    &map[string]interface{}{},
			expectedValue: map[string]interface{}{"key": "value"},
			expectError:   false,
		},
		{
			name:          "Unmarshal YAML to map",
			inputPayload:  payload.NewPayload("key: value", payload.YAML),
			targetType:    &map[string]interface{}{},
			expectedValue: map[string]interface{}{"key": "value"},
			expectError:   false,
		},
		{
			name: "Unmarshal JSON to struct",
			inputPayload: payload.NewPayload(`{
				"Name": "John Doe",
				"Age": 30,
				"Address": {
					"Street": "123 Main St",
					"City": "Anytown"
				}
			}`, payload.JSON),
			targetType: &struct {
				Name    string
				Age     int
				Address struct {
					Street string
					City   string
				}
			}{},
			expectedValue: struct {
				Name    string
				Age     int
				Address struct {
					Street string
					City   string
				}
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
			inputPayload: payload.NewPayload(`
name: "John Doe"
age: 30
address:
  street: 123 Main St
  city: Anytown
`, payload.YAML),
			targetType: &struct {
				Name    string
				Age     int
				Address struct {
					Street string
					City   string
				}
			}{},
			expectedValue: struct {
				Name    string
				Age     int
				Address struct {
					Street string
					City   string
				}
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
	transcoder := NewTranscoder()
	json.Register(transcoder)
	yaml.Register(transcoder)

	tests := []struct {
		name            string
		inputPayload    payload.Payload
		targetFormat    payload.Format
		expectedPayload payload.Payload
		expectError     bool
	}{
		{
			name:            "Transcode YAML to JSON",
			inputPayload:    payload.NewPayload("key: value", payload.YAML),
			targetFormat:    payload.JSON,
			expectedPayload: payload.NewPayload(`{"key":"value"}`, payload.JSON),
			expectError:     false,
		},
		{
			name: "Transcode YAML to JSON (complex)",
			inputPayload: payload.NewPayload(`
person:
  name: John Doe
  age: 30
  address:
    street: 123 Main St
    city: Anytown
`, payload.YAML),
			targetFormat:    payload.JSON,
			expectedPayload: payload.NewPayload(`{"person":{"address":{"city":"Anytown","street":"123 Main St"},"age":30,"name":"John Doe"}}`, payload.JSON),
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
				case payload.JSON:
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
				case payload.YAML, payload.Golang, payload.Lua, payload.String, payload.Bytes, payload.GoError:
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
	transcoder := NewTranscoder()
	json.Register(transcoder)
	yaml.Register(transcoder)

	tests := []struct {
		name          string
		inputPayload  payload.Payload
		targetType    interface{}
		expectedValue interface{}
		expectError   bool
	}{
		{
			name: "Unmarshal Golang ANY (from JSON) to struct",
			inputPayload: func() payload.Payload {
				// Spawn a JSON payload and then transcode it to Golang ANY
				jsonPayload := payload.NewPayload(`{
					"Name": "Jane Doe",
					"Age": 25,
					"Address": {
						"Street": "456 Oak Ave",
						"City": "Springfield"
					}
				}`, payload.JSON)
				golangAnyPayload, err := transcoder.Transcode(jsonPayload, payload.Golang)
				if err != nil {
					t.Fatalf("failed to transcode JSON to Golang ANY: %v", err)
				}
				return golangAnyPayload
			}(),
			targetType: &struct {
				Name    string
				Age     int
				Address struct {
					Street string
					City   string
				}
			}{},
			expectedValue: struct {
				Name    string
				Age     int
				Address struct {
					Street string
					City   string
				}
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
	transcoder := NewTranscoder()
	json.Register(transcoder)
	yaml.Register(transcoder)

	tests := []struct {
		name            string
		inputPayload    payload.Payload
		targetFormat    payload.Format
		expectedPayload payload.Payload
		expectError     bool
	}{
		{
			name:            "Transcode JSON to YAML",
			inputPayload:    payload.NewPayload(`{"key":"value"}`, payload.JSON),
			targetFormat:    payload.YAML,
			expectedPayload: payload.NewPayload("key: value\n", payload.YAML),
			expectError:     false,
		},
		{
			name:         "Transcode JSON to YAML (complex)",
			inputPayload: payload.NewPayload(`{"person":{"address":{"city":"Anytown","street":"123 Main St"},"age":30,"name":"John Doe"}}`, payload.JSON),
			targetFormat: payload.YAML,
			expectedPayload: payload.NewPayload(`person:
    address:
        city: Anytown
        street: 123 Main St
    age: 30
    name: John Doe
`, payload.YAML),
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
