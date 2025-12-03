// Package supervisor provides service lifecycle management and supervision.
package supervisor

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
)

func TestEventConstants(t *testing.T) {
	tests := []struct {
		name     string
		system   event.System
		kind     event.Kind
		expected string
	}{
		{"system", System, "", "supervisor"},
		{"register", "", ServiceRegister, "service.register"},
		{"remove", "", ServiceRemove, "service.remove"},
		{"update", "", ServiceUpdate, "service.update"},
		{"start", "", ServiceStart, "service.start"},
		{"stop", "", ServiceStop, "service.stop"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.system != "" {
				assert.Equal(t, tt.expected, tt.system)
			}
			if tt.kind != "" {
				assert.Equal(t, tt.expected, tt.kind)
			}
		})
	}
}

func TestStatusConstants(t *testing.T) {
	tests := []struct {
		name     string
		status   Status
		expected string
	}{
		{"unknown", StatusUnknown, "unknown"},
		{"starting", StatusStarting, "starting"},
		{"running", StatusRunning, "running"},
		{"stopping", StatusStopping, "stopping"},
		{"stopped", StatusStopped, "stopped"},
		{"exited", StatusExited, "exited"},
		{"failed", StatusFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status)
		})
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{"terminated", ErrTerminated, "service terminated"},
		{"exit", ErrExit, "service exited"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
			assert.True(t, errors.Is(tt.err, tt.err))
		})
	}
}

func TestEntry_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		entry   Entry
		wantErr bool
	}{
		{
			name: "complete entry",
			entry: Entry{
				Service: nil,
				Config:  LifecycleConfig{},
			},
			wantErr: false,
		},
		{
			name: "minimal entry",
			entry: Entry{
				Service: nil,
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

			var decoded Entry
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
		})
	}
}
