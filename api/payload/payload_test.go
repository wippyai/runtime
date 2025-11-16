// Package payload provides data encoding and transcoding.
package payload

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
)

func TestNewPayload(t *testing.T) {
	tests := []struct {
		name         string
		data         any
		format       Format
		expectData   any
		expectFormat Format
	}{
		{
			name:         "string data with JSON format",
			data:         `{"name": "test"}`,
			format:       JSON,
			expectData:   `{"name": "test"}`,
			expectFormat: JSON,
		},
		{
			name:         "nil data with Golang format",
			data:         nil,
			format:       Golang,
			expectData:   nil,
			expectFormat: Golang,
		},
		{
			name:         "struct with Golang format",
			data:         struct{ Name string }{"test"},
			format:       Golang,
			expectData:   struct{ Name string }{"test"},
			expectFormat: Golang,
		},
		{
			name:         "error with Error format",
			data:         errors.New("test error"),
			format:       Error,
			expectData:   errors.New("test error"),
			expectFormat: Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPayload(tt.data, tt.format)
			assert.Equal(t, tt.expectData, p.Data())
			assert.Equal(t, tt.expectFormat, p.Format())
		})
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name         string
		data         any
		expectData   any
		expectFormat Format
	}{
		{
			name:         "string data",
			data:         "test string",
			expectData:   "test string",
			expectFormat: Golang,
		},
		{
			name:         "integer data",
			data:         42,
			expectData:   42,
			expectFormat: Golang,
		},
		{
			name:         "nil data",
			data:         nil,
			expectData:   nil,
			expectFormat: Golang,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.data)
			assert.Equal(t, tt.expectData, p.Data())
			assert.Equal(t, tt.expectFormat, p.Format())
		})
	}
}

func TestNewString(t *testing.T) {
	tests := []struct {
		name         string
		data         string
		expectData   string
		expectFormat Format
	}{
		{
			name:         "empty string",
			data:         "",
			expectData:   "",
			expectFormat: String,
		},
		{
			name:         "non-empty string",
			data:         "test string",
			expectData:   "test string",
			expectFormat: String,
		},
		{
			name:         "multi-line string",
			data:         "line1\nline2",
			expectData:   "line1\nline2",
			expectFormat: String,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewString(tt.data)
			assert.Equal(t, tt.expectData, p.Data())
			assert.Equal(t, tt.expectFormat, p.Format())
		})
	}
}

func TestNewError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		expectData   error
		expectFormat Format
	}{
		{
			name:         "simple error",
			err:          errors.New("test error"),
			expectData:   errors.New("test error"),
			expectFormat: Error,
		},
		{
			name:         "nil error",
			err:          nil,
			expectData:   nil,
			expectFormat: Error,
		},
		{
			name:         "wrapped error",
			err:          fmt.Errorf("wrapped: %w", errors.New("original")),
			expectData:   fmt.Errorf("wrapped: %w", errors.New("original")),
			expectFormat: Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewError(tt.err)
			assert.Equal(t, tt.expectData, p.Data())
			assert.Equal(t, tt.expectFormat, p.Format())

			if tt.err != nil {
				assert.Equal(t, tt.err.Error(), p.Data().(error).Error())
			}
		})
	}
}

type mockTranscoder struct{}

func (m *mockTranscoder) Transcode(p Payload, f Format) (Payload, error) {
	return NewPayload(p.Data(), f), nil
}

func (m *mockTranscoder) Unmarshal(_ Payload, _ interface{}) error {
	return nil
}

func TestGetTranscoder(t *testing.T) {
	t.Run("returns nil when AppContext is nil", func(t *testing.T) {
		ctx := context.Background()
		transcoder := GetTranscoder(ctx)
		assert.Nil(t, transcoder)
	})

	t.Run("returns nil when transcoder not set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		transcoder := GetTranscoder(ctx)
		assert.Nil(t, transcoder)
	})

	t.Run("returns transcoder when set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		mockTc := &mockTranscoder{}
		ctx = WithTranscoder(ctx, mockTc)

		transcoder := GetTranscoder(ctx)
		require.NotNil(t, transcoder)
		assert.Equal(t, mockTc, transcoder)
	})
}

func TestWithTranscoder(t *testing.T) {
	t.Run("returns same context when AppContext is nil", func(t *testing.T) {
		ctx := context.Background()
		mockTc := &mockTranscoder{}
		newCtx := WithTranscoder(ctx, mockTc)

		assert.Equal(t, ctx, newCtx)
		assert.Nil(t, GetTranscoder(newCtx))
	})

	t.Run("attaches transcoder successfully", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())
		mockTc := &mockTranscoder{}

		ctx = WithTranscoder(ctx, mockTc)
		transcoder := GetTranscoder(ctx)

		require.NotNil(t, transcoder)
		assert.Equal(t, mockTc, transcoder)
	})

	t.Run("idempotent when transcoder already set", func(t *testing.T) {
		ctx := context.Background()
		ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())

		firstTc := &mockTranscoder{}
		ctx = WithTranscoder(ctx, firstTc)

		secondTc := &mockTranscoder{}
		ctx = WithTranscoder(ctx, secondTc)

		transcoder := GetTranscoder(ctx)
		require.NotNil(t, transcoder)
		assert.Equal(t, firstTc, transcoder)
	})
}
