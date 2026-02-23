// SPDX-License-Identifier: MPL-2.0

// Package store provides generic storage abstractions.
package store

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
			assert.True(t, errors.Is(tt.err, tt.err))
		})
	}
}

func TestError_Interface(t *testing.T) {
	t.Run("ErrKeyNotFound", func(t *testing.T) {
		assert.Equal(t, apierror.NotFound, ErrKeyNotFound.Kind())
		assert.Equal(t, apierror.False, ErrKeyNotFound.Retryable())
		assert.Nil(t, ErrKeyNotFound.Details())
	})

	t.Run("ErrKeyExists", func(t *testing.T) {
		assert.Equal(t, apierror.AlreadyExists, ErrKeyExists.Kind())
		assert.Equal(t, apierror.False, ErrKeyExists.Retryable())
	})

	t.Run("ErrInvalidKey", func(t *testing.T) {
		assert.Equal(t, apierror.Invalid, ErrInvalidKey.Kind())
		assert.Equal(t, apierror.False, ErrInvalidKey.Retryable())
	})
}

func TestCommandPools(t *testing.T) {
	t.Run("GetCmd pool", func(t *testing.T) {
		cmd := AcquireGetCmd()
		assert.NotNil(t, cmd)
		assert.Equal(t, Get, cmd.CmdID())

		cmd.Key = registry.NewID("test", "key")
		cmd.Release()

		cmd2 := AcquireGetCmd()
		assert.NotNil(t, cmd2)
		assert.Equal(t, registry.ID{}, cmd2.Key)
	})

	t.Run("SetCmd pool", func(t *testing.T) {
		cmd := AcquireSetCmd()
		assert.NotNil(t, cmd)
		assert.Equal(t, Set, cmd.CmdID())

		cmd.Entry = Entry{Key: registry.NewID("test", "key")}
		cmd.Release()

		cmd2 := AcquireSetCmd()
		assert.NotNil(t, cmd2)
		assert.Equal(t, Entry{}, cmd2.Entry)
	})

	t.Run("DeleteCmd pool", func(t *testing.T) {
		cmd := AcquireDeleteCmd()
		assert.NotNil(t, cmd)
		assert.Equal(t, Delete, cmd.CmdID())

		cmd.Key = registry.NewID("test", "key")
		cmd.Release()

		cmd2 := AcquireDeleteCmd()
		assert.NotNil(t, cmd2)
		assert.Equal(t, registry.ID{}, cmd2.Key)
	})

	t.Run("HasCmd pool", func(t *testing.T) {
		cmd := AcquireHasCmd()
		assert.NotNil(t, cmd)
		assert.Equal(t, Has, cmd.CmdID())

		cmd.Key = registry.NewID("test", "key")
		cmd.Release()

		cmd2 := AcquireHasCmd()
		assert.NotNil(t, cmd2)
		assert.Equal(t, registry.ID{}, cmd2.Key)
	})
}

func TestResponseTypes(t *testing.T) {
	t.Run("GetResponse", func(t *testing.T) {
		resp := GetResponse{Value: payload.New("test"), Error: nil}
		assert.NotNil(t, resp.Value)
		assert.NoError(t, resp.Error)

		resp2 := GetResponse{Error: ErrKeyNotFound}
		assert.Error(t, resp2.Error)
	})

	t.Run("SetResponse", func(t *testing.T) {
		resp := SetResponse{Error: nil}
		assert.NoError(t, resp.Error)

		resp2 := SetResponse{Error: ErrKeyExists}
		assert.Error(t, resp2.Error)
	})

	t.Run("DeleteResponse", func(t *testing.T) {
		resp := DeleteResponse{NotFound: false, Error: nil}
		assert.False(t, resp.NotFound)
		assert.NoError(t, resp.Error)

		resp2 := DeleteResponse{NotFound: true, Error: nil}
		assert.True(t, resp2.NotFound)
	})

	t.Run("HasResponse", func(t *testing.T) {
		resp := HasResponse{Exists: true, Error: nil}
		assert.True(t, resp.Exists)
		assert.NoError(t, resp.Error)

		resp2 := HasResponse{Exists: false, Error: nil}
		assert.False(t, resp2.Exists)
	})
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
				Key:   registry.NewID("cache", "user-123"),
				Value: payload.New(map[string]any{"name": "John Doe"}),
				TTL:   5 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "entry without TTL",
			entry: Entry{
				Key:   registry.NewID("data", "config"),
				Value: payload.New("configuration data"),
				TTL:   0,
			},
			wantErr: false,
		},
		{
			name: "minimal entry",
			entry: Entry{
				Key:   registry.NewID("store", "item"),
				Value: payload.New("item"),
			},
			wantErr: false,
		},
		{
			name: "entry with long TTL",
			entry: Entry{
				Key:   registry.NewID("persistent", "data"),
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

// Benchmarks

func BenchmarkAcquireGetCmd(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cmd := AcquireGetCmd()
		cmd.Key = registry.NewID("test", "key")
		cmd.Release()
	}
}

func BenchmarkAcquireSetCmd(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cmd := AcquireSetCmd()
		cmd.Entry = Entry{Key: registry.NewID("test", "key")}
		cmd.Release()
	}
}

func BenchmarkAcquireDeleteCmd(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cmd := AcquireDeleteCmd()
		cmd.Key = registry.NewID("test", "key")
		cmd.Release()
	}
}

func BenchmarkAcquireHasCmd(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cmd := AcquireHasCmd()
		cmd.Key = registry.NewID("test", "key")
		cmd.Release()
	}
}
