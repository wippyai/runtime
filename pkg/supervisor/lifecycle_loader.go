package supervisor

import (
	"fmt"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/supervisor"
)

// LifecycleLoader manages the loading of lifecycle configurations from payloads.
type LifecycleLoader struct {
	dtt payload.Transcoder
}

// NewLifecycleLoader creates a new LifecycleLoader.
func NewLifecycleLoader(dtt payload.Transcoder) *LifecycleLoader {
	return &LifecycleLoader{
		dtt: dtt,
	}
}

// Load extracts the lifecycle configuration from the payload.
func (l *LifecycleLoader) Load(p payload.Payload) (supervisor.Lifecycle, error) {
	var raw struct {
		Lifecycle supervisor.Lifecycle `json:"lifecycle" yaml:"lifecycle"`
	}

	err := l.dtt.Unmarshal(p, &raw)
	if err != nil {
		return supervisor.Lifecycle{}, fmt.Errorf("failed to unmarshal payload for lifecycle extraction: %w", err)
	}

	return raw.Lifecycle, nil
}
