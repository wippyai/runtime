package json

import (
	"reflect"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
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
			payload: payload.NewPayload(make(chan int), payload.Golang), // Channels cannot be marshalled
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
