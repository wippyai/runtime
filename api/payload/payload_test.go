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
	apierror "github.com/wippyai/runtime/api/error"
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
			format:       GoError,
			expectData:   errors.New("test error"),
			expectFormat: GoError,
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
			expectFormat: GoError,
		},
		{
			name:         "nil error",
			err:          nil,
			expectData:   nil,
			expectFormat: GoError,
		},
		{
			name:         "wrapped error",
			err:          fmt.Errorf("wrapped: %w", errors.New("original")),
			expectData:   fmt.Errorf("wrapped: %w", errors.New("original")),
			expectFormat: GoError,
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

func TestErrorInterface(t *testing.T) {
	t.Run("ErrEmptyFormat", func(t *testing.T) {
		err := ErrEmptyFormat
		assert.Equal(t, "payload format is empty", err.Error())
		assert.Equal(t, apierror.KindInvalid, err.Kind())
		assert.Equal(t, apierror.False, err.Retryable())
		assert.Nil(t, err.Details())
		assert.Nil(t, err.Unwrap())
	})
}

func TestErrorConstructors(t *testing.T) {
	t.Run("NewNoTranscodingPathError", func(t *testing.T) {
		err := NewNoTranscodingPathError(JSON, Golang)
		assert.Contains(t, err.Error(), "json")
		assert.Contains(t, err.Error(), "golang")
		assert.Equal(t, apierror.KindNotFound, err.Kind())
		from, _ := err.Details().Get("from")
		assert.Equal(t, string(JSON), from)
		to, _ := err.Details().Get("to")
		assert.Equal(t, string(Golang), to)
	})

	t.Run("NewNoTranscoderError", func(t *testing.T) {
		err := NewNoTranscoderError("json", "golang")
		assert.Contains(t, err.Error(), "no transcoder registered")
		assert.Equal(t, apierror.KindNotFound, err.Kind())
	})

	t.Run("NewTranscodeError", func(t *testing.T) {
		cause := errors.New("test error")
		err := NewTranscodeError("json", "golang", cause)
		assert.Contains(t, err.Error(), "transcoding")
		assert.Equal(t, apierror.KindInternal, err.Kind())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("NewNoUnmarshalPathError", func(t *testing.T) {
		err := NewNoUnmarshalPathError(JSON)
		assert.Contains(t, err.Error(), "unmarshaling path")
		assert.Equal(t, apierror.KindNotFound, err.Kind())
	})

	t.Run("NewUnmarshalTranscodeError", func(t *testing.T) {
		cause := errors.New("test error")
		err := NewUnmarshalTranscodeError(cause)
		assert.Contains(t, err.Error(), "transcoding payload")
		assert.Equal(t, apierror.KindInternal, err.Kind())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("NewUnmarshalerNotFoundError", func(t *testing.T) {
		err := NewUnmarshalerNotFoundError("json")
		assert.Contains(t, err.Error(), "unmarshaler not found")
		assert.Equal(t, apierror.KindInternal, err.Kind())
	})

	t.Run("NewInvalidFormatError", func(t *testing.T) {
		err := NewInvalidFormatError("input", JSON, Golang)
		assert.Contains(t, err.Error(), "input")
		assert.Equal(t, apierror.KindInvalid, err.Kind())
		direction, _ := err.Details().Get("direction")
		assert.Equal(t, "input", direction)
	})

	t.Run("NewInvalidDataTypeError", func(t *testing.T) {
		err := NewInvalidDataTypeError("input", "string", "int")
		assert.Contains(t, err.Error(), "input")
		assert.Contains(t, err.Error(), "string")
		assert.Equal(t, apierror.KindInvalid, err.Kind())
	})

	t.Run("NewMarshalError", func(t *testing.T) {
		cause := errors.New("marshal failed")
		err := NewMarshalError("json", cause)
		assert.Contains(t, err.Error(), "marshal")
		assert.Contains(t, err.Error(), "json")
		assert.Equal(t, apierror.KindInternal, err.Kind())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("NewUnmarshalError", func(t *testing.T) {
		cause := errors.New("unmarshal failed")
		err := NewUnmarshalError("json", cause)
		assert.Contains(t, err.Error(), "unmarshal")
		assert.Contains(t, err.Error(), "json")
		assert.Equal(t, apierror.KindInvalid, err.Kind())
		assert.Equal(t, cause, err.Unwrap())
	})
}
