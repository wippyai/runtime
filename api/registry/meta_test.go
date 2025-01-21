package registry

import (
	"reflect"
	"testing"
)

func TestMetadata_StringValue(t *testing.T) {
	tests := []struct {
		name     string
		metadata Metadata
		key      string
		want     string
	}{
		{
			name:     "existing string value",
			metadata: Metadata{"key": "value"},
			key:      "key",
			want:     "value",
		},
		{
			name:     "non-existent key",
			metadata: Metadata{},
			key:      "missing",
			want:     "",
		},
		{
			name:     "wrong type value",
			metadata: Metadata{"key": 123},
			key:      "key",
			want:     "",
		},
		{
			name:     "nil metadata",
			metadata: nil,
			key:      "key",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.metadata.StringValue(tt.key); got != tt.want {
				t.Errorf("Metadata.StringValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMetadata_IntValue(t *testing.T) {
	tests := []struct {
		name     string
		metadata Metadata
		key      string
		want     int
	}{
		{
			name:     "existing int value",
			metadata: Metadata{"key": 42},
			key:      "key",
			want:     42,
		},
		{
			name:     "non-existent key",
			metadata: Metadata{},
			key:      "missing",
			want:     0,
		},
		{
			name:     "wrong type value",
			metadata: Metadata{"key": "123"},
			key:      "key",
			want:     0,
		},
		{
			name:     "nil metadata",
			metadata: nil,
			key:      "key",
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.metadata.IntValue(tt.key); got != tt.want {
				t.Errorf("Metadata.IntValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMetadata_BoolValue(t *testing.T) {
	tests := []struct {
		name     string
		metadata Metadata
		key      string
		want     bool
	}{
		{
			name:     "existing bool value true",
			metadata: Metadata{"key": true},
			key:      "key",
			want:     true,
		},
		{
			name:     "existing bool value false",
			metadata: Metadata{"key": false},
			key:      "key",
			want:     false,
		},
		{
			name:     "non-existent key",
			metadata: Metadata{},
			key:      "missing",
			want:     false,
		},
		{
			name:     "wrong type value",
			metadata: Metadata{"key": "true"},
			key:      "key",
			want:     false,
		},
		{
			name:     "nil metadata",
			metadata: nil,
			key:      "key",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.metadata.BoolValue(tt.key); got != tt.want {
				t.Errorf("Metadata.BoolValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMetadata_TagValue(t *testing.T) {
	tests := []struct {
		name     string
		metadata Metadata
		key      string
		want     []string
	}{
		{
			name:     "existing string slice",
			metadata: Metadata{"key": []string{"tag1", "tag2"}},
			key:      "key",
			want:     []string{"tag1", "tag2"},
		},
		{
			name:     "single string value",
			metadata: Metadata{"key": "tag1"},
			key:      "key",
			want:     []string{"tag1"},
		},
		{
			name:     "slice of interface",
			metadata: Metadata{"key": []any{"tag1", "tag2"}},
			key:      "key",
			want:     []string{"tag1", "tag2"},
		},
		{
			name:     "mixed type slice",
			metadata: Metadata{"key": []any{"tag1", 123, "tag2"}},
			key:      "key",
			want:     []string{"tag1", "", "tag2"},
		},
		{
			name:     "non-existent key",
			metadata: Metadata{},
			key:      "missing",
			want:     nil,
		},
		{
			name:     "wrong type value",
			metadata: Metadata{"key": 123},
			key:      "key",
			want:     nil,
		},
		{
			name:     "nil metadata",
			metadata: nil,
			key:      "key",
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.metadata.TagValue(tt.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Metadata.TagValue() = %v, want %v", got, tt.want)
			}
		})
	}
}
