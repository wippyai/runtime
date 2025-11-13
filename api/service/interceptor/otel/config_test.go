// Package otel provides OpenTelemetry interceptor configuration.
package otel

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "enabled",
			config: Config{
				Enabled: true,
			},
			wantErr: false,
		},
		{
			name: "disabled",
			config: Config{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name:    "default",
			config:  Config{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded Config
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config, decoded)
		})
	}
}

func TestOptions_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		options Options
		wantErr bool
	}{
		{
			name: "complete options",
			options: Options{
				SpanName: "my-operation",
				Attributes: map[string]string{
					"service": "api",
					"method":  "GET",
				},
			},
			wantErr: false,
		},
		{
			name: "span name only",
			options: Options{
				SpanName: "process",
			},
			wantErr: false,
		},
		{
			name: "attributes only",
			options: Options{
				Attributes: map[string]string{
					"key": "value",
				},
			},
			wantErr: false,
		},
		{
			name:    "empty options",
			options: Options{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.options)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded Options
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.options, decoded)
		})
	}
}
