// set.go
package template

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/CloudyKit/jet/v6"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/service/template"
)

// TemplateSet represents a set of templates with shared configuration
//
//nolint:revive // name TemplateSet used for better readability
type TemplateSet struct {
	id      registry.ID
	jetSet  *jet.Set
	loader  *jet.InMemLoader
	config  *template.SetConfig
	dtt     payload.Transcoder
	mu      sync.RWMutex
	sources map[string]string // Store template sources by name (not ID)
}

// NewTemplateSet creates a new template set with the given configuration
func NewTemplateSet(id registry.ID, config *template.SetConfig, dtt payload.Transcoder) (*TemplateSet, error) {
	// Create a loader for in-memory templates
	loader := jet.NewInMemLoader()

	// Prepare options for the Jet set
	var options []jet.Option

	if config.Engine.DevelopmentMode {
		options = append(options, jet.InDevelopmentMode())
	}

	if config.Engine.Delimiters.Left != "" && config.Engine.Delimiters.Right != "" {
		options = append(options, jet.WithDelims(
			config.Engine.Delimiters.Left,
			config.Engine.Delimiters.Right,
		))
	}

	// Create the Jet set with the loader and options
	jetSet := jet.NewSet(loader, options...)

	// Add globals
	for key, value := range config.Engine.Globals {
		jetSet.AddGlobal(key, value)
	}

	return &TemplateSet{
		id:      id,
		jetSet:  jetSet,
		loader:  loader,
		config:  config,
		dtt:     dtt,
		sources: make(map[string]string),
	}, nil
}

// ID returns the set ID
func (s *TemplateSet) ID() registry.ID {
	return s.id
}

// Config returns the set configuration
func (s *TemplateSet) Config() *template.SetConfig {
	return s.config
}

// AddTemplate adds a new template to the set
func (s *TemplateSet) AddTemplate(name string, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if template already exists
	if _, exists := s.sources[name]; exists {
		return fmt.Errorf("template %s already exists in set", name)
	}

	// Register the template with Jet's InMemLoader
	s.loader.Set(name, source)

	// Store the source
	s.sources[name] = source
	return nil
}

// UpdateTemplate updates an existing template in the set
func (s *TemplateSet) UpdateTemplate(name string, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if template exists
	if _, exists := s.sources[name]; !exists {
		return ErrTemplateNotFound
	}

	// Update the template in Jet's loader
	s.loader.Set(name, source)

	// Update the source
	s.sources[name] = source
	return nil
}

// RemoveTemplate removes a template from the set
func (s *TemplateSet) RemoveTemplate(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if template exists
	if _, exists := s.sources[name]; !exists {
		return ErrTemplateNotFound
	}

	// Delete the template from Jet's loader
	s.loader.Delete(name)

	// Remove the source
	delete(s.sources, name)
	return nil
}

// GetTemplateSource gets a template source from the set by name
func (s *TemplateSet) GetTemplateSource(name string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	source, exists := s.sources[name]
	if !exists {
		return "", ErrTemplateNotFound
	}

	return source, nil
}

// GetAllTemplates returns all template names and sources in the set
func (s *TemplateSet) GetAllTemplates() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create a copy to avoid race conditions
	result := make(map[string]string, len(s.sources))
	for name, source := range s.sources {
		result[name] = source
	}

	return result
}

// RenderTemplate renders a template by name with the given variables
func (s *TemplateSet) RenderTemplate(name string, vars map[string]any) (string, error) {
	s.mu.RLock()
	_, exists := s.sources[name]
	s.mu.RUnlock()

	if !exists {
		return "", ErrTemplateNotFound
	}

	// Get the Jet template
	jetTemplate, err := s.jetSet.GetTemplate(name)
	if err != nil {
		return "", fmt.Errorf("failed to get compiled template: %w", err)
	}

	// Create a buffer for the output
	var buf bytes.Buffer

	// Create a variables map in the format expected by Jet
	jetVars := make(jet.VarMap)
	for k, v := range vars {
		jetVars.Set(k, v)
	}

	// Render the template
	if err := jetTemplate.Execute(&buf, jetVars, nil); err != nil {
		return "", fmt.Errorf("%w: %w", ErrRenderFailed, err)
	}

	return buf.String(), nil
}

// RenderPayload renders a template with data from a payload
func (s *TemplateSet) RenderPayload(name string, data payload.Payload) (string, error) {
	// Extract data from payload using transcoder
	var vars any
	if err := s.dtt.Unmarshal(data, &vars); err != nil {
		return "", fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	mvars, ok := vars.(map[string]any)
	if !ok {
		mvars = make(map[string]any)
	}

	// Render the template
	result, err := s.RenderTemplate(name, mvars)
	if err != nil {
		return "", err
	}

	// Return a new payload with the rendered template as a string
	return result, nil
}

// Acquire implements resource.Provider
func (s *TemplateSet) Acquire(
	_ context.Context,
	_ registry.ID,
	mode resource.AccessMode,
) (resource.Resource[any], error) {
	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, fmt.Errorf("unsupported access mode: %v", mode)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create a template wrapper just for this request
	return &setResource{set: s}, nil
}

// setResource represents a resource wrapper for a template set
type setResource struct {
	set    *TemplateSet
	mu     sync.Mutex
	closed bool
}

// Get implements resource.Resource
func (r *setResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, resource.ErrResourceReleased
	}

	return r.set, nil
}

// Release implements resource.Resource
func (r *setResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closed = true
}
