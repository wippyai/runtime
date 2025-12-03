// Package jet provides Jet template engine implementation
package jet

import (
	"context"
	"errors"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/service/template"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"go.uber.org/zap"
)

// Manager handles template lifecycle and provisioning
type Manager struct {
	log        *zap.Logger
	dtt        payload.Transcoder
	bus        event.Bus
	mu         sync.RWMutex
	sets       map[registry.ID]*Set
	setConfigs map[registry.ID]*template.SetConfig
	templates  map[registry.ID]templateEntry
}

// templateEntry tracks information about registered templates
type templateEntry struct {
	ID     registry.ID
	SetID  registry.ID
	Source string
	Name   string // The name used within the set
}

// NewManager creates a new template manager
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *Manager {
	return &Manager{
		log:        log,
		dtt:        dtt,
		bus:        bus,
		sets:       make(map[registry.ID]*Set),
		setConfigs: make(map[registry.ID]*template.SetConfig),
		templates:  make(map[registry.ID]templateEntry),
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
		return newUnsupportedKindError(string(entry.Kind))
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
		return newUnsupportedKindError(string(entry.Kind))
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
		return newUnsupportedKindError(string(entry.Kind))
	}
}

// handleTemplateAdd adds a new template to its corresponding set
func (m *Manager) handleTemplateAdd(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, err := entryutil.DecodeEntryConfig[template.Config](ctx, m.dtt, entry)
	if err != nil {
		return newDecodeConfigError(err)
	}
	if entry.Meta != nil {
		cfg.Meta = entry.Meta
	}

	// Set namespace if not specified
	cfg.Set = cfg.Set.WithDefaultNS(entry.ID.NS)

	// Get the template set
	set, exists := m.sets[cfg.Set]
	if !exists {
		return newSetNotFoundError(cfg.Set.String())
	}

	// Determine template name (using meta name or ID name as fallback)
	templateName := cfg.Meta.GetString("name", "")
	if templateName == "" {
		templateName = entry.ID.Name
	}

	// Check if template already exists in the set
	if _, err := set.GetTemplateSource(templateName); err == nil {
		return newTemplateExistsError(templateName, cfg.Set.String())
	}

	// Create and add the template to the set
	if err := set.AddTemplate(templateName, cfg.Source); err != nil {
		return newCreateTemplateError(err)
	}

	// Store template entry
	m.templates[entry.ID] = templateEntry{
		ID:     entry.ID,
		SetID:  cfg.Set,
		Source: cfg.Source,
		Name:   templateName,
	}

	m.log.Debug("template added",
		zap.String("id", entry.ID.String()),
		zap.String("set", cfg.Set.String()),
		zap.String("name", templateName))

	return nil
}

// handleTemplateUpdate updates an existing template in its set
func (m *Manager) handleTemplateUpdate(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if template exists
	existingTemplate, exists := m.templates[entry.ID]
	if !exists {
		return newTemplateNotFoundError(entry.ID.String())
	}

	cfg, err := entryutil.DecodeEntryConfig[template.Config](ctx, m.dtt, entry)
	if err != nil {
		return newDecodeConfigError(err)
	}

	if entry.Meta != nil {
		cfg.Meta = entry.Meta
	}

	// Set namespace if not specified
	cfg.Set = cfg.Set.WithDefaultNS(entry.ID.NS)

	// Check if the template set has changed
	if cfg.Set != existingTemplate.SetID {
		// Template is moving to a new set

		// Get the source set
		sourceSet, exists := m.sets[existingTemplate.SetID]
		if !exists {
			return newSetNotFoundError(existingTemplate.SetID.String())
		}

		// Get the target set
		targetSet, exists := m.sets[cfg.Set]
		if !exists {
			return newSetNotFoundError(cfg.Set.String())
		}

		// Determine template name for the target set (using meta name or ID name as fallback)
		newTemplateName := cfg.Meta.GetString("name", "")
		if newTemplateName == "" {
			newTemplateName = entry.ID.Name
		}

		// Check if template already exists in the target set
		if _, err := targetSet.GetTemplateSource(newTemplateName); err == nil {
			return newTemplateExistsError(newTemplateName, cfg.Set.String())
		}

		// Remove from source set
		if err := sourceSet.RemoveTemplate(existingTemplate.Name); err != nil {
			if !errors.Is(err, template.ErrTemplateNotFound) {
				return newRemoveTemplateError(err)
			}
		}

		// Add to target set
		if err := targetSet.AddTemplate(newTemplateName, cfg.Source); err != nil {
			return newAddTemplateError(err)
		}

		// Update the template entry
		m.templates[entry.ID] = templateEntry{
			ID:     entry.ID,
			SetID:  cfg.Set,
			Source: cfg.Source,
			Name:   newTemplateName,
		}

		m.log.Info("template moved to new set",
			zap.String("id", entry.ID.String()),
			zap.String("from_set", existingTemplate.SetID.String()),
			zap.String("to_set", cfg.Set.String()),
			zap.String("name", newTemplateName))
	} else {
		// Template remains in the same set
		set := m.sets[cfg.Set]

		// Determine if the template name has changed
		newTemplateName := cfg.Meta.GetString("name", "")
		if newTemplateName == "" {
			newTemplateName = entry.ID.Name
		}

		if newTemplateName != existingTemplate.Name {
			// Template name is changing

			// Check if new name already exists
			if _, err := set.GetTemplateSource(newTemplateName); err == nil {
				return newTemplateNameExistsError(newTemplateName, cfg.Set.String())
			}

			// Remove old template
			if err := set.RemoveTemplate(existingTemplate.Name); err != nil {
				if !errors.Is(err, template.ErrTemplateNotFound) {
					return newRemoveOldTemplateError(err)
				}
			}

			// Add with new name
			if err := set.AddTemplate(newTemplateName, cfg.Source); err != nil {
				return newAddTemplateWithNewNameError(err)
			}
		} else {
			// Just update the template source
			if err := set.UpdateTemplate(existingTemplate.Name, cfg.Source); err != nil {
				return newUpdateTemplateError(err)
			}
		}

		// Update the template entry
		m.templates[entry.ID] = templateEntry{
			ID:     entry.ID,
			SetID:  cfg.Set,
			Source: cfg.Source,
			Name:   newTemplateName,
		}

		m.log.Info("template updated",
			zap.String("id", entry.ID.String()),
			zap.String("set", cfg.Set.String()),
			zap.String("name", newTemplateName))
	}

	return nil
}

// handleTemplateDelete removes a template from its set
func (m *Manager) handleTemplateDelete(_ context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find template entry
	tplEntry, exists := m.templates[entry.ID]
	if !exists {
		return newTemplateNotFoundError(entry.ID.String())
	}

	// Get the set
	set, exists := m.sets[tplEntry.SetID]
	if !exists {
		// If set doesn't exist, just remove from our registry
		delete(m.templates, entry.ID)
		return nil
	}

	// Remove the template from the set
	if err := set.RemoveTemplate(tplEntry.Name); err != nil {
		if !errors.Is(err, template.ErrTemplateNotFound) {
			return newDeleteTemplateError(err)
		}
	}

	// Remove from our registry
	delete(m.templates, entry.ID)

	m.log.Info("template deleted",
		zap.String("id", entry.ID.String()),
		zap.String("set", tplEntry.SetID.String()),
		zap.String("name", tplEntry.Name))

	return nil
}

// handleSetAdd adds a new template set
func (m *Manager) handleSetAdd(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if set already exists
	if _, exists := m.sets[entry.ID]; exists {
		return newSetAlreadyExistsError(entry.ID.String())
	}

	cfg, err := entryutil.DecodeEntryConfig[template.SetConfig](ctx, m.dtt, entry)
	if err != nil {
		return newSetConfigDecodeError(err)
	}

	// Create the template set
	set, err := NewSet(entry.ID, cfg, m.dtt)
	if err != nil {
		return newCreateSetError(err)
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
		return newSetNotFoundError(entry.ID.String())
	}

	// Decode configuration
	cfg, err := entryutil.DecodeEntryConfig[template.SetConfig](ctx, m.dtt, entry)
	if err != nil {
		return newSetConfigDecodeError(err)
	}

	// Create a new template set with updated configuration
	set, err := NewSet(entry.ID, cfg, m.dtt)
	if err != nil {
		return newUpdateSetError(err)
	}

	// Migrate all templates from the existing set to the new one
	templates := existingSet.GetAllTemplates()
	for name, source := range templates {
		if err := set.AddTemplate(name, source); err != nil {
			return newMigrateTemplateError(name, err)
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
		return newSetNotFoundError(entry.ID.String())
	}

	// Check if the set has any templates
	templates := set.GetAllTemplates()
	if len(templates) > 0 {
		return newSetNotEmptyError(entry.ID.String(), len(templates))
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
func (m *Manager) GetTemplateSet(id registry.ID) (*Set, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	set, exists := m.sets[id]
	if !exists {
		return nil, newSetNotFoundError(id.String())
	}

	return set, nil
}

// Acquire implements resource.Provider
func (m *Manager) Acquire(
	ctx context.Context,
	id registry.ID,
	mode resource.AccessMode,
) (resource.Resource[any], error) {
	// Find the set that this ID refers to
	set, err := m.GetTemplateSet(id)
	if err == nil {
		// It's a set, forward the acquisition
		return set.Acquire(ctx, id, mode)
	}

	// It might be a template
	m.mu.RLock()
	entry, exists := m.templates[id]
	if !exists {
		m.mu.RUnlock()
		return nil, newTemplateNotFoundError(id.String())
	}

	set, exists = m.sets[entry.SetID]
	if !exists {
		m.mu.RUnlock()
		return nil, newSetNotFoundError(entry.SetID.String())
	}
	m.mu.RUnlock()

	// Create a resource for the template
	return set.Acquire(ctx, registry.NewID("", entry.Name), mode)
}
