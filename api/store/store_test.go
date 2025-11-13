// Package store provides generic storage abstractions.
package store

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{"key not found", ErrKeyNotFound, "key not found"},
		{"key exists", ErrKeyExists, "key already exists"},
		{"invalid key", ErrInvalidKey, "invalid key format"},
		{"store full", ErrStoreFull, "store is full, cannot add more entries"},
		{"store closed", ErrStoreClosed, "store is closed for operations, cannot perform action"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
			assert.True(t, errors.Is(tt.err, tt.err))
		})
	}
}

func TestEntry_Marshal(t *testing.T) {
	tests := []struct {
		name    string
		entry   Entry
		wantErr bool
	}{
		{
			name: "complete entry",
			entry: Entry{
				Key:   registry.ID{NS: "cache", Name: "user-123"},
				Value: payload.New(map[string]any{"name": "John Doe"}),
				TTL:   5 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "entry without TTL",
			entry: Entry{
				Key:   registry.ID{NS: "data", Name: "config"},
				Value: payload.New("configuration data"),
				TTL:   0,
			},
			wantErr: false,
		},
		{
			name: "minimal entry",
			entry: Entry{
				Key:   registry.ID{NS: "store", Name: "item"},
				Value: payload.New("item"),
			},
			wantErr: false,
		},
		{
			name: "entry with long TTL",
			entry: Entry{
				Key:   registry.ID{NS: "persistent", Name: "data"},
				Value: payload.New([]int{1, 2, 3, 4, 5}),
				TTL:   24 * time.Hour,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.entry)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotEmpty(t, data)
		})
	}
}
