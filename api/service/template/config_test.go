// Package template provides template service configuration.
package template

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

func TestKindConstants(t *testing.T) {
	tests := []struct {
		name     string
		kind     registry.Kind
		expected string
	}{
		{"template", KindTemplate, "template.jet"},
		{"template set", KindTemplateSet, "template.set"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.kind)
		})
	}
}

func TestConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "complete config",
			config: Config{
				Meta:   attrs.Bag{"type": "template"},
				Source: "template content",
				Set:    registry.NewID("templates", "main"),
			},
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
			assert.Equal(t, tt.config.Source, decoded.Source)
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: Config{
				Source: "template",
				Set:    registry.NewID("t", "main"),
			},
			wantErr: false,
		},
		{
			name: "empty source",
			config: Config{
				Set: registry.NewID("t", "main"),
			},
			wantErr: true,
			errMsg:  "source cannot be empty",
		},
		{
			name: "empty set name",
			config: Config{
				Source: "template",
				Set:    registry.ID{NS: "t"},
			},
			wantErr: true,
			errMsg:  "set name cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEngineConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  EngineConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid with defaults",
			config:  EngineConfig{},
			wantErr: false,
		},
		{
			name: "conflicting left delimiters",
			config: EngineConfig{
				Delimiters: DelimiterConfig{
					Left:         "{{",
					Right:        "}}",
					CommentLeft:  "{{",
					CommentRight: "*}",
				},
				Extensions: []string{".jet"},
			},
			wantErr: true,
			errMsg:  "must be different",
		},
		{
			name: "conflicting right delimiters",
			config: EngineConfig{
				Delimiters: DelimiterConfig{
					Left:         "{{",
					Right:        "}}",
					CommentLeft:  "{*",
					CommentRight: "}}",
				},
				Extensions: []string{".jet"},
			},
			wantErr: true,
			errMsg:  "must be different",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSetConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  SetConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: SetConfig{
				Engine: EngineConfig{},
			},
			wantErr: false,
		},
		{
			name: "invalid engine config - conflicting delimiters",
			config: SetConfig{
				Engine: EngineConfig{
					Delimiters: DelimiterConfig{
						Left:         "{{",
						Right:        "}}",
						CommentLeft:  "{{",
						CommentRight: "*}",
					},
					Extensions: []string{".jet"},
				},
			},
			wantErr: true,
			errMsg:  "must be different",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
