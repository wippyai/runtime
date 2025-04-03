package template

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/CloudyKit/jet/v6"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/service/template"
)

// TemplateSet represents a set of templates with shared configuration
type TemplateSet struct {
	id      registry.ID
	jetSet  *jet.Set
	loader  *jet.InMemLoader
	config  *template.SetConfig
	dtt     payload.Transcoder
	mu      sync.RWMutex
	sources map[registry.ID]string // Store template sources by ID
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
		sources: make(map[registry.ID]string),
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
func (s *TemplateSet) AddTemplate(id registry.ID, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if template already exists
	if _, exists := s.sources[id]; exists {
		return fmt.Errorf("template %s already exists in set", id)
	}

	// Register the template with Jet's InMemLoader
	tmplName := id.String()
	s.loader.Set(tmplName, source)

	// Store the source
	s.sources[id] = source
	return nil
}

// UpdateTemplate updates an existing template in the set
func (s *TemplateSet) UpdateTemplate(id registry.ID, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if template exists
	if _, exists := s.sources[id]; !exists {
		return ErrTemplateNotFound
	}

	// Update the template in Jet's loader
	tmplName := id.String()
	s.loader.Set(tmplName, source)

	// Update the source
	s.sources[id] = source
	return nil
}

// RemoveTemplate removes a template from the set
func (s *TemplateSet) RemoveTemplate(id registry.ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if template exists
	if _, exists := s.sources[id]; !exists {
		return ErrTemplateNotFound
	}

	// Delete the template from Jet's loader
	tmplName := id.String()
	s.loader.Delete(tmplName)

	// Remove the source
	delete(s.sources, id)
	return nil
}

// GetTemplateSource gets a template source from the set by ID
func (s *TemplateSet) GetTemplateSource(id registry.ID) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	source, exists := s.sources[id]
	if !exists {
		return "", ErrTemplateNotFound
	}

	return source, nil
}

// GetAllTemplates returns all template IDs and sources in the set
func (s *TemplateSet) GetAllTemplates() map[registry.ID]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create a copy to avoid race conditions
	result := make(map[registry.ID]string, len(s.sources))
	for id, source := range s.sources {
		result[id] = source
	}

	return result
}

// RenderTemplate renders a template by ID with the given variables
func (s *TemplateSet) RenderTemplate(id registry.ID, vars map[string]interface{}) (string, error) {
	s.mu.RLock()
	_, exists := s.sources[id]
	s.mu.RUnlock()

	if !exists {
		return "", ErrTemplateNotFound
	}

	// Get the Jet template
	jetTemplate, err := s.jetSet.GetTemplate(id.String())
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
		return "", fmt.Errorf("%w: %v", ErrRenderFailed, err)
	}

	return buf.String(), nil
}

// RenderPayload renders a template with data from a payload
func (s *TemplateSet) RenderPayload(id registry.ID, data payload.Payload) (payload.Payload, error) {
	// Extract data from payload using transcoder
	var vars map[string]interface{}
	if err := s.dtt.Unmarshal(data, &vars); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	// Render the template
	result, err := s.RenderTemplate(id, vars)
	if err != nil {
		return nil, err
	}

	// Return a new payload with the rendered template as a string
	return payload.NewString(result), nil
}

// Acquire implements resource.Provider
func (s *TemplateSet) Acquire(
	_ context.Context,
	id registry.ID,
	mode resource.AccessMode,
) (resource.Resource[any], error) {
	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, fmt.Errorf("unsupported access mode: %v", mode)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// If the ID is the set's ID, provide the set itself
	if id == s.id {
		return &templateSetResource{
			set: s,
		}, nil
	}

	// Otherwise, check if we have a template with that ID
	if _, exists := s.sources[id]; !exists {
		return nil, ErrTemplateNotFound
	}

	// Create a template wrapper just for this request
	return &templateResource{
		set: s,
		id:  id,
	}, nil
}

// templateSetResource represents a resource wrapper for a template set
type templateSetResource struct {
	set    *TemplateSet
	mu     sync.Mutex
	closed bool
}

// Get implements resource.Resource
func (r *templateSetResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, resource.ErrResourceReleased
	}

	return r.set, nil
}

// Release implements resource.Resource
func (r *templateSetResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closed = true
}

// templateResource represents a resource wrapper for a template ID
type templateResource struct {
	set    *TemplateSet
	id     registry.ID
	mu     sync.Mutex
	closed bool
}

// Get implements resource.Resource
func (r *templateResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, resource.ErrResourceReleased
	}

	// Return a map with template details
	return map[string]interface{}{
		"id":    r.id,
		"setId": r.set.id,
	}, nil
}

// Release implements resource.Resource
func (r *templateResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closed = true
}
