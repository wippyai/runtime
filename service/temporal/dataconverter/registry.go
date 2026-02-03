package dataconverter

import (
	"sync"

	"go.temporal.io/sdk/converter"
)

// Registry manages data converter codecs for Temporal clients
type Registry struct {
	base   converter.DataConverter
	codecs []converter.PayloadCodec
	mu     sync.RWMutex
}

// NewRegistry creates a new data converter registry with a base converter
func NewRegistry(base converter.DataConverter) *Registry {
	return &Registry{
		codecs: make([]converter.PayloadCodec, 0),
		base:   base,
	}
}

// RegisterCodec adds a custom payload codec to the registry
func (r *Registry) RegisterCodec(codec converter.PayloadCodec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.codecs = append(r.codecs, codec)
}

// Build creates the final data converter with all registered codecs chained
func (r *Registry) Build() converter.DataConverter {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.codecs) == 0 {
		return r.base
	}

	return converter.NewCodecDataConverter(r.base, r.codecs...)
}
