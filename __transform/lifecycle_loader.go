package __transform

import (
	"fmt"
	"github.com/ponyruntime/pony/__transform/api"

	"github.com/ponyruntime/pony/api/payload"
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
func (l *LifecycleLoader) Load(p payload.Payload) (api.Lifecycle, error) {
	var raw struct {
		Lifecycle api.Lifecycle `json:"lifecycle" yaml:"lifecycle"`
	}

	err := l.dtt.Unmarshal(p, &raw)
	if err != nil {
		return api.Lifecycle{}, fmt.Errorf("failed to unmarshal payload for lifecycle extraction: %w", err)
	}

	return raw.Lifecycle, nil
}
