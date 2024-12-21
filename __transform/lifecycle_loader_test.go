package __transform

import (
	"github.com/ponyruntime/pony/__transform/api"
	"github.com/ponyruntime/pony/pkg/payload/yaml"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	transcoder "github.com/ponyruntime/pony/pkg/payload"
	"github.com/ponyruntime/pony/pkg/payload/json"
	"github.com/stretchr/testify/assert"
)

func TestLifecycleLoader_Load(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    api.Lifecycle
		wantErr bool
	}{
		{
			name: "Valid YAML",
			input: `
lifecycle:
  auto_start: true
  restart:
    delay: 5s
    max_attempts: 3
`,
			want: api.Lifecycle{
				AutoStart: true,
				Restart: api.RetryPolicy{
					Delay:       "5s",
					MaxAttempts: 3,
				},
			},
			wantErr: false,
		},
		{
			name: "Valid JSON",
			input: `
{
  "lifecycle": {
    "auto_start": true,
    "restart": {
      "delay": "5s",
      "max_attempts": 3
    }
  }
}
`,
			want: api.Lifecycle{
				AutoStart: true,
				Restart: api.RetryPolicy{
					Delay:       "5s",
					MaxAttempts: 3,
				},
			},
			wantErr: false,
		},
		{
			name:    "Invalid YAML",
			input:   `invalid yaml`,
			want:    api.Lifecycle{},
			wantErr: true,
		},
		{
			name: "Invalid JSON",
			input: `{
  "lifecycle": {
    "auto_start": true,
    "restart": {
      "delay": "5s",
      "max_attempts": "invalid"
    }
  }
}`,
			want:    api.Lifecycle{},
			wantErr: true,
		},
		{
			name:    "Empty Input",
			input:   ``,
			want:    api.Lifecycle{},
			wantErr: false, // Empty lifecycle is valid
		},
		{
			name: "Missing Lifecycle",
			input: `
name: web_server
kind: http.server
meta:
  server_id: "default"
`,
			want:    api.Lifecycle{},
			wantErr: false, // Missing lifecycle is valid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a transcoder
			dtt := createTestTranscoder()

			// Create a LifecycleLoader
			loader := NewLifecycleLoader(dtt)

			// Create a payload
			p := payload.NewPayload(tt.input, payload.Yaml)

			// Load the lifecycle
			got, err := loader.Load(p)
			if (err != nil) != tt.wantErr {
				t.Errorf("LifecycleLoader.Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			assert.Equal(t, tt.want, got, "Loaded lifecycle does not match expected value")
		})
	}
}

func createTestTranscoder() payload.Transcoder {
	tr := transcoder.NewTranscoder()
	json.Register(tr)
	yaml.Register(tr)
	return tr
}
