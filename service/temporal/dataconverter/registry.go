// SPDX-License-Identifier: MPL-2.0

package dataconverter

import (
	"sync"

	"go.temporal.io/sdk/converter"
)

// Registry collects payload codecs and chains them onto a base DataConverter.
// Thread-safe for concurrent RegisterCodec and Build calls.
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

// Build returns the base converter wrapped with all registered codecs.
// Returns the base converter directly when no codecs are registered.
func (r *Registry) Build() converter.DataConverter {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.codecs) == 0 {
		return r.base
	}

	return converter.NewCodecDataConverter(r.base, r.codecs...)
}
