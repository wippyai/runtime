package yaml

import (
	"reflect"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
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
			payload: payload.NewPayload("key: value", payload.Yaml),
			want:    payload.NewPayload(map[string]interface{}{"key": "value"}, payload.Golang),
			wantErr: false,
		},
		{
			name:    "Valid YAML bytes",
			payload: payload.NewPayload([]byte("key: value"), payload.Yaml),
			want:    payload.NewPayload(map[string]interface{}{"key": "value"}, payload.Golang),
			wantErr: false,
		},
		{
			name:    "Invalid YAML",
			payload: payload.NewPayload("key: value\ninvalid", payload.Yaml),
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
			payload: payload.NewPayload(123, payload.Yaml),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Invalid YAML bytes",
			payload: payload.NewPayload([]byte("key: value\ninvalid"), payload.Yaml),
			want:    nil,
			wantErr: true,
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

	tests := []struct {
		name    string
		payload payload.Payload
		target  interface{}
		want    interface{}
		wantErr bool
	}{
		{
			name:    "Valid YAML string to struct",
			payload: payload.NewPayload("key: value", payload.Yaml),
			target:  &TestStruct{},
			want:    &TestStruct{Key: "value"},
			wantErr: false,
		},
		{
			name:    "Valid YAML bytes to struct",
			payload: payload.NewPayload([]byte("key: value"), payload.Yaml),
			target:  &TestStruct{},
			want:    &TestStruct{Key: "value"},
			wantErr: false,
		},
		{
			name:    "Invalid YAML",
			payload: payload.NewPayload("key: value\ninvalid", payload.Yaml),
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
			payload: payload.NewPayload("key: value", payload.Yaml),
			target:  "not a pointer",
			want:    "",
			wantErr: true,
		},
		{
			name:    "Unsupported unmarshal data type",
			payload: payload.NewPayload(123, payload.Yaml),
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
			want:    payload.NewPayload("key: value\n", payload.Yaml),
			wantErr: false,
		},
		{
			name:    "Valid map",
			payload: payload.NewPayload(map[string]string{"key": "value"}, payload.Golang),
			want:    payload.NewPayload("key: value\n", payload.Yaml),
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
			payload: payload.NewPayload(make(chan int), payload.Golang), // Channels cannot be marshalled
			want:    nil,
			wantErr: true,
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
