package payload

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
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
			name:         "string Data_ with JSON Format_",
			data:         `{"name": "test"}`,
			format:       JSON,
			expectData:   `{"name": "test"}`,
			expectFormat: JSON,
		},
		{
			name:         "nil Data_ with Golang Format_",
			data:         nil,
			format:       Golang,
			expectData:   nil,
			expectFormat: Golang,
		},
		{
			name:         "struct with Golang Format_",
			data:         struct{ Name string }{"test"},
			format:       Golang,
			expectData:   struct{ Name string }{"test"},
			expectFormat: Golang,
		},
		{
			name:         "error with Error Format_",
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
			name:         "string Data_",
			data:         "test string",
			expectData:   "test string",
			expectFormat: Golang,
		},
		{
			name:         "integer Data_",
			data:         42,
			expectData:   42,
			expectFormat: Golang,
		},
		{
			name:         "nil Data_",
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
