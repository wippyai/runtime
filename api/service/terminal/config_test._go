package terminal

import (
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/stretchr/testify/assert"
)

func TestServiceConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ServiceConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: ServiceConfig{
				Meta:   registry.Metadata{"version": "1.0"},
				Target: "test-terminal",
				Lifecycle: supervisor.LifecycleConfig{
					StartTimeout: 30 * time.Second,
					StopTimeout:  60 * time.Second,
				},
			},
			wantErr: false,
		},
		{
			name: "nil metadata",
			config: ServiceConfig{
				Target: "test-terminal",
			},
			wantErr: true,
		},
		{
			name: "empty target",
			config: ServiceConfig{
				Meta: registry.Metadata{"version": "1.0"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
