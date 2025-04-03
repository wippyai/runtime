package template

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/service/template"
	"go.uber.org/zap"
)

var (
	// ErrTemplateNotFound is returned when a template cannot be found
	ErrTemplateNotFound = errors.New("template not found")

	// ErrSetNotFound is returned when a template set cannot be found
	ErrSetNotFound = errors.New("template set not found")

	// ErrRenderFailed is returned when template rendering fails
	ErrRenderFailed = errors.New("template rendering failed")

	// ErrInvalidSource is returned when a template source is invalid
	ErrInvalidSource = errors.New("invalid template source")

	// ErrSetNotEmpty is returned when attempting to delete a set with templates
	ErrSetNotEmpty = errors.New("template set is not empty")
)

// Error represents a template-specific error
type Error struct {
	Template string
	Err      error
}

// Error returns the error message
func (e *Error) Error() string {
	return fmt.Sprintf("template error in %s: %v", e.Template, e.Err)
}

// Unwrap returns the underlying error
func (e *Error) Unwrap() error {
	return e.Err
}

// Manager handles template lifecycle and provisioning
type Manager struct {
	log        *zap.Logger
	dtt        payload.Transcoder
	bus        event.Bus
	mu         sync.RWMutex
	sets       map[registry.ID]*TemplateSet
	setConfigs map[registry.ID]*template.SetConfig
}

// NewManager creates a new template manager
func NewManager(
	dtt payload.Transcoder,
	bus event.Bus,
	log *zap.Logger,
) *Manager {
	return &Manager{
		log:        log,
		dtt:        dtt,
		bus:        bus,
		sets:       make(map[registry.ID]*TemplateSet),
		setConfigs: make(map[registry.ID]*template.SetConfig),
	}
}

// Add implements registry.EntryListener - registers a template or set
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case template.KindTemplate:
		return m.handleTemplateAdd(ctx, entry)
	case template.KindTemplateSet:
		return m.handleSetAdd(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// Update implements registry.EntryListener - updates a template or set
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case template.KindTemplate:
		return m.handleTemplateUpdate(ctx, entry)
	case template.KindTemplateSet:
		return m.handleSetUpdate(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// Delete implements registry.EntryListener - removes a template or set
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case template.KindTemplate:
		return m.handleTemplateDelete(ctx, entry)
	case template.KindTemplateSet:
		return m.handleSetDelete(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// handleTemplateAdd adds a new template to its corresponding set
func (m *Manager) handleTemplateAdd(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Decode configuration
	cfg := new(template.Config)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal template config: %w", err)
	}

	// Initialize defaults and validate
	cfg.InitDefaults()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid template config: %w", err)
	}

	// Set namespace if not specified
	cfg.Set = cfg.Set.WithDefaultNS(entry.ID.NS)

	// Get the template set
	set, exists := m.sets[cfg.Set]
	if !exists {
		return fmt.Errorf("%w: %s", ErrSetNotFound, cfg.Set)
	}

	// Check if template already exists in the set
	if _, err := set.GetTemplateSource(entry.ID); err == nil {
		return fmt.Errorf("template %s already exists in set %s", entry.ID, cfg.Set)
	}

	// Create and add the template to the set
	if err := set.AddTemplate(entry.ID, cfg.Source); err != nil {
		return fmt.Errorf("failed to create template: %w", err)
	}

	m.log.Info("template added",
		zap.String("id", entry.ID.String()),
		zap.String("set", cfg.Set.String()))

	return nil
}

// handleTemplateUpdate updates an existing template in its set
func (m *Manager) handleTemplateUpdate(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Decode configuration
	cfg := new(template.Config)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal template config: %w", err)
	}

	// Initialize defaults and validate
	cfg.InitDefaults()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid template config: %w", err)
	}

	// Set namespace if not specified
	cfg.Set = cfg.Set.WithDefaultNS(entry.ID.NS)

	// Get the template set
	set, exists := m.sets[cfg.Set]
	if !exists {
		return fmt.Errorf("%w: %s", ErrSetNotFound, cfg.Set)
	}

	// Update the template in the set
	if err := set.UpdateTemplate(entry.ID, cfg.Source); err != nil {
		return fmt.Errorf("failed to update template: %w", err)
	}

	m.log.Info("template updated",
		zap.String("id", entry.ID.String()),
		zap.String("set", cfg.Set.String()))

	return nil
}

// handleTemplateDelete removes a template from its set
func (m *Manager) handleTemplateDelete(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find which set contains this template
	var foundSet *TemplateSet
	for _, set := range m.sets {
		if _, err := set.GetTemplateSource(entry.ID); err == nil {
			foundSet = set
			break
		}
	}

	if foundSet == nil {
		return fmt.Errorf("%w: %s", ErrTemplateNotFound, entry.ID)
	}

	// Remove the template from the set
	if err := foundSet.RemoveTemplate(entry.ID); err != nil {
		return fmt.Errorf("failed to remove template: %w", err)
	}

	m.log.Info("template deleted",
		zap.String("id", entry.ID.String()))

	return nil
}

// handleSetAdd adds a new template set
func (m *Manager) handleSetAdd(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if set already exists
	if _, exists := m.sets[entry.ID]; exists {
		return fmt.Errorf("template set %s already exists", entry.ID)
	}

	// Decode configuration
	cfg := new(template.SetConfig)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal set config: %w", err)
	}

	// Initialize defaults and validate
	cfg.InitDefaults()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid set config: %w", err)
	}

	// Create the template set
	set, err := NewTemplateSet(entry.ID, cfg, m.dtt)
	if err != nil {
		return fmt.Errorf("failed to create template set: %w", err)
	}

	// Store the set and its configuration
	m.sets[entry.ID] = set
	m.setConfigs[entry.ID] = cfg

	// Register as resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID:       entry.ID,
			Provider: set,
			Meta:     entry.Meta,
		},
	})

	m.log.Info("template set added",
		zap.String("id", entry.ID.String()))

	return nil
}

// handleSetUpdate updates an existing template set
func (m *Manager) handleSetUpdate(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if set exists
	existingSet, exists := m.sets[entry.ID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrSetNotFound, entry.ID)
	}

	// Decode configuration
	cfg := new(template.SetConfig)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal set config: %w", err)
	}

	// Initialize defaults and validate
	cfg.InitDefaults()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid set config: %w", err)
	}

	// Create a new template set with updated configuration
	set, err := NewTemplateSet(entry.ID, cfg, m.dtt)
	if err != nil {
		return fmt.Errorf("failed to update template set: %w", err)
	}

	// Migrate all templates from the existing set to the new one
	templates := existingSet.GetAllTemplates()
	for id, source := range templates {
		if err := set.AddTemplate(id, source); err != nil {
			return fmt.Errorf("failed to migrate template %s: %w", id, err)
		}
	}

	// Update the set and its configuration
	m.sets[entry.ID] = set
	m.setConfigs[entry.ID] = cfg

	// Update resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Update,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID:       entry.ID,
			Provider: set,
			Meta:     entry.Meta,
		},
	})

	m.log.Info("template set updated",
		zap.String("id", entry.ID.String()))

	return nil
}

// handleSetDelete removes a template set
func (m *Manager) handleSetDelete(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if set exists
	set, exists := m.sets[entry.ID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrSetNotFound, entry.ID)
	}

	// Check if the set has any templates
	templates := set.GetAllTemplates()
	if len(templates) > 0 {
		return fmt.Errorf("%w: set %s contains %d templates",
			ErrSetNotEmpty, entry.ID, len(templates))
	}

	// Unregister resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   entry.ID.String(),
		Data:   entry.ID,
	})

	// Remove the set and its configuration
	delete(m.sets, entry.ID)
	delete(m.setConfigs, entry.ID)

	m.log.Info("template set deleted",
		zap.String("id", entry.ID.String()))

	return nil
}

// GetTemplateSet retrieves a template set by ID
func (m *Manager) GetTemplateSet(id registry.ID) (*TemplateSet, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	set, exists := m.sets[id]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrSetNotFound, id)
	}

	return set, nil
}
