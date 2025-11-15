// Package registry provides service registry and entry management.
package registry

import (
	"reflect"
	"testing"

	"github.com/wippyai/runtime/api/attrs"
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
			metadata: attrs.NewBagFrom(map[string]any{"key": "value"}),
			key:      "key",
			want:     "value",
		},
		{
			name:     "non-existent key",
			metadata: attrs.NewBag(),
			key:      "missing",
			want:     "",
		},
		{
			name:     "wrong type value",
			metadata: attrs.NewBagFrom(map[string]any{"key": 123}),
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
			if got := tt.metadata.GetString(tt.key, ""); got != tt.want {
				t.Errorf("Metadata.GetString() = %v, want %v", got, tt.want)
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
			metadata: attrs.NewBagFrom(map[string]any{"key": 42}),
			key:      "key",
			want:     42,
		},
		{
			name:     "non-existent key",
			metadata: attrs.NewBag(),
			key:      "missing",
			want:     0,
		},
		{
			name:     "wrong type value",
			metadata: attrs.NewBagFrom(map[string]any{"key": "123"}),
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
			if got := tt.metadata.GetInt(tt.key, 0); got != tt.want {
				t.Errorf("Metadata.GetInt() = %v, want %v", got, tt.want)
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
			metadata: attrs.NewBagFrom(map[string]any{"key": true}),
			key:      "key",
			want:     true,
		},
		{
			name:     "existing bool value false",
			metadata: attrs.NewBagFrom(map[string]any{"key": false}),
			key:      "key",
			want:     false,
		},
		{
			name:     "non-existent key",
			metadata: attrs.NewBag(),
			key:      "missing",
			want:     false,
		},
		{
			name:     "wrong type value",
			metadata: attrs.NewBagFrom(map[string]any{"key": "true"}),
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
			if got := tt.metadata.GetBool(tt.key, false); got != tt.want {
				t.Errorf("Metadata.GetBool() = %v, want %v", got, tt.want)
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
			metadata: attrs.NewBagFrom(map[string]any{"key": []string{"tag1", "tag2"}}),
			key:      "key",
			want:     []string{"tag1", "tag2"},
		},
		{
			name:     "single string value",
			metadata: attrs.NewBagFrom(map[string]any{"key": "tag1"}),
			key:      "key",
			want:     []string{"tag1"},
		},
		{
			name:     "slice of interface",
			metadata: attrs.NewBagFrom(map[string]any{"key": []any{"tag1", "tag2"}}),
			key:      "key",
			want:     []string{"tag1", "tag2"},
		},
		{
			name:     "mixed type slice",
			metadata: attrs.NewBagFrom(map[string]any{"key": []any{"tag1", 123, "tag2"}}),
			key:      "key",
			want:     []string{"tag1", "", "tag2"},
		},
		{
			name:     "non-existent key",
			metadata: attrs.NewBag(),
			key:      "missing",
			want:     nil,
		},
		{
			name:     "wrong type value",
			metadata: attrs.NewBagFrom(map[string]any{"key": 123}),
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
			got := tt.metadata.GetSlice(tt.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Metadata.GetSlice() = %v, want %v", got, tt.want)
			}
		})
	}
}
