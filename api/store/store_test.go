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
		{"store full", ErrStoreFull, "store is full"},
		{"store closed", ErrStoreClosed, "store is closed"},
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
		assert.Equal(t, apierror.KindNotFound, ErrKeyNotFound.Kind())
		assert.Equal(t, apierror.False, ErrKeyNotFound.Retryable())
		assert.Nil(t, ErrKeyNotFound.Details())
		assert.Nil(t, ErrKeyNotFound.Unwrap())
	})

	t.Run("ErrKeyExists", func(t *testing.T) {
		assert.Equal(t, apierror.KindAlreadyExists, ErrKeyExists.Kind())
		assert.Equal(t, apierror.False, ErrKeyExists.Retryable())
	})

	t.Run("ErrStoreFull", func(t *testing.T) {
		assert.Equal(t, apierror.KindUnavailable, ErrStoreFull.Kind())
		assert.Equal(t, apierror.True, ErrStoreFull.Retryable())
	})

	t.Run("ErrStoreClosed", func(t *testing.T) {
		assert.Equal(t, apierror.KindUnavailable, ErrStoreClosed.Kind())
		assert.Equal(t, apierror.False, ErrStoreClosed.Retryable())
	})

	t.Run("ErrInvalidKey", func(t *testing.T) {
		assert.Equal(t, apierror.KindInvalid, ErrInvalidKey.Kind())
		assert.Equal(t, apierror.False, ErrInvalidKey.Retryable())
	})
}

func TestNewKeyNotFoundError(t *testing.T) {
	key := registry.NewID("test", "mykey")
	err := NewKeyNotFoundError(key)

	assert.Equal(t, "key not found", err.Error())
	assert.Equal(t, apierror.KindNotFound, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.NotNil(t, err.Details())

	keyVal, ok := err.Details().Get("key")
	assert.True(t, ok)
	assert.Equal(t, key.String(), keyVal)
}

func TestNewKeyExistsError(t *testing.T) {
	key := registry.NewID("test", "existing")
	err := NewKeyExistsError(key)

	assert.Equal(t, "key already exists", err.Error())
	assert.Equal(t, apierror.KindAlreadyExists, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.NotNil(t, err.Details())

	keyVal, ok := err.Details().Get("key")
	assert.True(t, ok)
	assert.Equal(t, key.String(), keyVal)
}

func TestNewInvalidKeyError(t *testing.T) {
	err := NewInvalidKeyError("bad-key", "contains invalid characters")

	assert.Equal(t, "invalid key format: contains invalid characters", err.Error())
	assert.Equal(t, apierror.KindInvalid, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.NotNil(t, err.Details())

	keyVal, ok := err.Details().Get("key")
	assert.True(t, ok)
	assert.Equal(t, "bad-key", keyVal)

	reasonVal, ok := err.Details().Get("reason")
	assert.True(t, ok)
	assert.Equal(t, "contains invalid characters", reasonVal)
}

func TestNewUnsupportedKindError(t *testing.T) {
	err := NewUnsupportedKindError("unknown.kind")

	assert.Equal(t, "unsupported entry kind: unknown.kind", err.Error())
	assert.Equal(t, apierror.KindInvalid, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.NotNil(t, err.Details())

	kindVal, ok := err.Details().Get("kind")
	assert.True(t, ok)
	assert.Equal(t, "unknown.kind", kindVal)
}

func TestNewStoreAlreadyExistsError(t *testing.T) {
	err := NewStoreAlreadyExistsError("test:mystore")

	assert.Equal(t, "store test:mystore already exists", err.Error())
	assert.Equal(t, apierror.KindAlreadyExists, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.NotNil(t, err.Details())

	idVal, ok := err.Details().Get("id")
	assert.True(t, ok)
	assert.Equal(t, "test:mystore", idVal)
}

func TestNewStoreNotFoundError(t *testing.T) {
	err := NewStoreNotFoundError("test:missing")

	assert.Equal(t, "store test:missing not found", err.Error())
	assert.Equal(t, apierror.KindNotFound, err.Kind())
	assert.Equal(t, apierror.False, err.Retryable())
	assert.NotNil(t, err.Details())

	idVal, ok := err.Details().Get("id")
	assert.True(t, ok)
	assert.Equal(t, "test:missing", idVal)
}

func TestCommandPools(t *testing.T) {
	t.Run("GetCmd pool", func(t *testing.T) {
		cmd := AcquireGetCmd()
		assert.NotNil(t, cmd)
		assert.Equal(t, CmdStoreGet, cmd.CmdID())

		cmd.Key = registry.NewID("test", "key")
		cmd.Release()

		cmd2 := AcquireGetCmd()
		assert.NotNil(t, cmd2)
		assert.Equal(t, registry.ID{}, cmd2.Key)
	})

	t.Run("SetCmd pool", func(t *testing.T) {
		cmd := AcquireSetCmd()
		assert.NotNil(t, cmd)
		assert.Equal(t, CmdStoreSet, cmd.CmdID())

		cmd.Entry = Entry{Key: registry.NewID("test", "key")}
		cmd.Release()

		cmd2 := AcquireSetCmd()
		assert.NotNil(t, cmd2)
		assert.Equal(t, Entry{}, cmd2.Entry)
	})

	t.Run("DeleteCmd pool", func(t *testing.T) {
		cmd := AcquireDeleteCmd()
		assert.NotNil(t, cmd)
		assert.Equal(t, CmdStoreDelete, cmd.CmdID())

		cmd.Key = registry.NewID("test", "key")
		cmd.Release()

		cmd2 := AcquireDeleteCmd()
		assert.NotNil(t, cmd2)
		assert.Equal(t, registry.ID{}, cmd2.Key)
	})

	t.Run("HasCmd pool", func(t *testing.T) {
		cmd := AcquireHasCmd()
		assert.NotNil(t, cmd)
		assert.Equal(t, CmdStoreHas, cmd.CmdID())

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

		resp2 := SetResponse{Error: ErrStoreFull}
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
