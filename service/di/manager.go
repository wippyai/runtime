package di

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/contract"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	apidi "github.com/ponyruntime/pony/api/service/di"
	"go.uber.org/zap"
)

// Manager handles contract registry entries and forwards them to the contract system plane
type Manager struct {
	log *zap.Logger
	dtt payload.Transcoder
	bus event.Bus

	definitions map[registry.ID]*contract.Definition
	bindings    map[registry.ID]*contract.Binding
	mu          sync.RWMutex
}

// NewManager creates a new contract manager
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *Manager {
	return &Manager{
		log:         log,
		dtt:         dtt,
		bus:         bus,
		definitions: make(map[registry.ID]*contract.Definition),
		bindings:    make(map[registry.ID]*contract.Binding),
	}
}

// Add handles the registration of new contract definitions and bindings
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case apidi.KindDefinition:
		return m.handleDefinitionAdd(ctx, entry)
	case apidi.KindBinding:
		return m.handleBindingAdd(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// Update handles updates to existing contract definitions and bindings
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case apidi.KindDefinition:
		return m.handleDefinitionUpdate(ctx, entry)
	case apidi.KindBinding:
		return m.handleBindingUpdate(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// Delete handles removal of contract definitions and bindings
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case apidi.KindDefinition:
		return m.handleDefinitionDelete(ctx, entry)
	case apidi.KindBinding:
		return m.handleBindingDelete(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// --- Validation Helpers ---

// validateDefinitionStructure checks the internal consistency of a definition.
func (m *Manager) validateDefinitionStructure(def *contract.Definition, defID registry.ID) error {
	methodNames := make(map[string]struct{})
	if def.Methods == nil { // A contract with no methods is valid.
		return nil
	}
	for _, method := range def.Methods {
		if method.Name == "" {
			return fmt.Errorf("method name cannot be empty in definition '%s'", defID)
		}
		if _, exists := methodNames[method.Name]; exists {
			return fmt.Errorf("duplicate method name '%s' in definition '%s'", method.Name, defID)
		}
		methodNames[method.Name] = struct{}{}

		// Validate InputSchemas: if definition exists, format must exist.
		for i, inputSchema := range method.InputSchemas {
			hasInputDef := false
			if inputSchema.Definition != nil {
				if rawMsg, ok := inputSchema.Definition.(json.RawMessage); ok {
					s := string(rawMsg)
					// Consider "null", empty string from empty RawMessage, or "{}" as effectively no "actual" definition data.
					if s != "null" && s != "" && s != "{}" {
						hasInputDef = true
					}
				} else { // If it's not json.RawMessage but not nil, it implies some definition content.
					hasInputDef = true
				}
			}
			if hasInputDef && inputSchema.Format == "" {
				return fmt.Errorf("input schema %d for method '%s' in definition '%s' has a definition but no format specified", i, method.Name, defID)
			}
		}

		// Validate OutputSchemas: if definition exists, format must exist.
		for i, outputSchema := range method.OutputSchemas {
			hasOutputDef := false
			if outputSchema.Definition != nil {
				if rawMsg, ok := outputSchema.Definition.(json.RawMessage); ok {
					s := string(rawMsg)
					if s != "null" && s != "" && s != "{}" {
						hasOutputDef = true
					}
				} else {
					hasOutputDef = true
				}
			}
			if hasOutputDef && outputSchema.Format == "" {
				return fmt.Errorf("output schema %d for method '%s' in definition '%s' has a definition but no format specified", i, method.Name, defID)
			}
		}
	}
	return nil
}

// validateBindingAgainstDefinitions checks if a binding is valid with the current set of definitions.
// Assumes m.mu is RLock'd or Lock'd by the caller appropriately for m.definitions access.
func (m *Manager) validateBindingAgainstDefinitions(binding *contract.Binding, bindingID registry.ID) error {
	if len(binding.Contracts) == 0 {
		return fmt.Errorf("binding '%s' must bind at least one contract", bindingID)
	}
	for i, bc := range binding.Contracts {
		contractDef, exists := m.definitions[bc.Contract]
		if !exists {
			return fmt.Errorf("binding '%s' (contract index %d, ID '%s'): contract definition not found", bindingID, i, bc.Contract)
		}

		// Check method completeness: all methods in definition must be bound.
		defMethodNames := make(map[string]struct{})
		for _, methodDef := range contractDef.Methods {
			defMethodNames[methodDef.Name] = struct{}{}
			if _, bound := bc.Methods[methodDef.Name]; !bound {
				return fmt.Errorf("binding '%s' (contract '%s'): method '%s' defined in contract is not bound", bindingID, bc.Contract, methodDef.Name)
			}
		}

		// Check for extraneous methods: all bound methods must exist in definition.
		for methodName := range bc.Methods {
			if _, defined := defMethodNames[methodName]; !defined {
				return fmt.Errorf("binding '%s' (contract '%s'): bound method '%s' is not defined in contract definition", bindingID, bc.Contract, methodName)
			}
		}
	}
	return nil
}

// validateUniqueDefaults checks that no contract has multiple default bindings
// This ensures that each contract can have at most one default binding, preventing conflicts
// when using default binding resolution (contract:open() without binding ID)
// Assumes m.mu is RLock'd or Lock'd by the caller appropriately for m.bindings access.
func (m *Manager) validateUniqueDefaults(binding *contract.Binding, bindingID registry.ID) error {
	for _, bc := range binding.Contracts {
		if bc.Default {
			// Check if another binding already has default for this contract
			for otherBindingID, otherBinding := range m.bindings {
				if otherBindingID == bindingID {
					continue // Skip self
				}
				for _, otherBC := range otherBinding.Contracts {
					if otherBC.Contract == bc.Contract && otherBC.Default {
						return fmt.Errorf("contract '%s' already has default binding '%s', cannot set binding '%s' as default",
							bc.Contract, otherBindingID, bindingID)
					}
				}
			}
		}
	}
	return nil
}

// --- Contract Definition handlers ---

func (m *Manager) handleDefinitionAdd(ctx context.Context, entry registry.Entry) error {
	cfg, err := m.decodeDefinition(entry)
	if err != nil {
		return fmt.Errorf("failed to decode definition '%s': %w", entry.ID, err)
	}
	definition := cfg.ToDefinition()

	// Set ID and Meta from entry
	definition.ID = entry.ID
	definition.Meta = entry.Meta

	if err := m.validateDefinitionStructure(definition, entry.ID); err != nil {
		return err // Error already includes ID
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.definitions[entry.ID]; exists {
		return fmt.Errorf("contract definition '%s' already exists", entry.ID)
	}

	m.definitions[entry.ID] = definition

	m.bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterDefinition,
		Path:   entry.ID.String(),
		Data:   definition,
	})

	m.log.Info("contract definition registered",
		zap.String("id", entry.ID.String()),
		zap.Int("methods", len(definition.Methods)))
	return nil
}

func (m *Manager) handleDefinitionUpdate(ctx context.Context, entry registry.Entry) error {
	cfg, err := m.decodeDefinition(entry)
	if err != nil {
		return fmt.Errorf("failed to decode definition for update '%s': %w", entry.ID, err)
	}
	updatedDefinition := cfg.ToDefinition()

	// Set ID and Meta from entry
	updatedDefinition.ID = entry.ID
	updatedDefinition.Meta = entry.Meta

	if err := m.validateDefinitionStructure(updatedDefinition, entry.ID); err != nil {
		return err // Error already includes ID
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	originalDefinition, exists := m.definitions[entry.ID]
	if !exists {
		return fmt.Errorf("contract definition '%s' not found for update", entry.ID)
	}

	// Temporarily apply the update to check dependent bindings
	m.definitions[entry.ID] = updatedDefinition
	var validationError error
	for bindingID, binding := range m.bindings {
		usesUpdatedDef := false
		for _, boundContract := range binding.Contracts {
			if boundContract.Contract == entry.ID {
				usesUpdatedDef = true
				break
			}
		}
		if usesUpdatedDef {
			// Re-validate this binding against the *new* definition
			if err := m.validateBindingAgainstDefinitions(binding, bindingID); err != nil {
				validationError = fmt.Errorf("updating definition '%s' would invalidate binding '%s': %w", entry.ID, bindingID, err)
				break
			}
		}
	}

	if validationError != nil {
		m.definitions[entry.ID] = originalDefinition // Rollback
		return validationError
	}
	// If successful, updatedDefinition remains in m.definitions[entry.ID]

	m.bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.UpdateDefinition,
		Path:   entry.ID.String(),
		Data:   updatedDefinition,
	})

	m.log.Info("contract definition updated",
		zap.String("id", entry.ID.String()),
		zap.Int("methods", len(updatedDefinition.Methods)))
	return nil
}

func (m *Manager) handleDefinitionDelete(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.definitions[entry.ID]; !exists {
		return fmt.Errorf("contract definition '%s' not found for deletion", entry.ID)
	}

	// Check if any binding refers to this definition
	for bindingID, binding := range m.bindings {
		for _, boundContract := range binding.Contracts {
			if boundContract.Contract == entry.ID {
				return fmt.Errorf("cannot delete contract definition '%s': it is used by binding '%s'", entry.ID, bindingID)
			}
		}
	}

	delete(m.definitions, entry.ID)

	m.bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.DeleteDefinition,
		Path:   entry.ID.String(),
	})

	m.log.Info("contract definition deleted", zap.String("id", entry.ID.String()))
	return nil
}

// --- Contract Binding handlers ---

func (m *Manager) handleBindingAdd(ctx context.Context, entry registry.Entry) error {
	cfg, err := m.decodeBinding(entry)
	if err != nil {
		return fmt.Errorf("failed to decode binding '%s': %w", entry.ID, err)
	}
	binding := cfg.ToBinding()

	// Set ID and Meta from entry
	binding.ID = entry.ID
	binding.Meta = entry.Meta

	m.mu.Lock() // Lock for m.bindings write and m.definitions read
	defer m.mu.Unlock()

	if _, exists := m.bindings[entry.ID]; exists {
		return fmt.Errorf("contract binding '%s' already exists", entry.ID)
	}

	// validateBindingAgainstDefinitions needs read access to m.definitions, which is covered by the Lock
	if err := m.validateBindingAgainstDefinitions(binding, entry.ID); err != nil {
		return err // Error from validateBinding already includes bindingID
	}

	// Validate unique defaults - needs read access to m.bindings, which is covered by the Lock
	// This prevents multiple bindings from being marked as default for the same contract
	if err := m.validateUniqueDefaults(binding, entry.ID); err != nil {
		return err
	}

	m.bindings[entry.ID] = binding

	m.bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.RegisterBinding,
		Path:   entry.ID.String(),
		Data:   binding,
	})

	m.log.Info("contract binding registered",
		zap.String("id", entry.ID.String()),
		zap.Int("contracts", len(binding.Contracts)))
	return nil
}

func (m *Manager) handleBindingUpdate(ctx context.Context, entry registry.Entry) error {
	cfg, err := m.decodeBinding(entry)
	if err != nil {
		return fmt.Errorf("failed to decode binding for update '%s': %w", entry.ID, err)
	}
	updatedBinding := cfg.ToBinding()

	// Set ID and Meta from entry
	updatedBinding.ID = entry.ID
	updatedBinding.Meta = entry.Meta

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.bindings[entry.ID]; !exists {
		return fmt.Errorf("contract binding '%s' not found for update", entry.ID)
	}

	if err := m.validateBindingAgainstDefinitions(updatedBinding, entry.ID); err != nil {
		return err // Error from validateBinding already includes bindingID
	}

	// Validate unique defaults for the updated binding
	// This ensures that updating a binding to set default=true doesn't conflict with existing defaults
	if err := m.validateUniqueDefaults(updatedBinding, entry.ID); err != nil {
		return err
	}

	m.bindings[entry.ID] = updatedBinding

	m.bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.UpdateBinding,
		Path:   entry.ID.String(),
		Data:   updatedBinding,
	})

	m.log.Info("contract binding updated",
		zap.String("id", entry.ID.String()),
		zap.Int("contracts", len(updatedBinding.Contracts)))
	return nil
}

func (m *Manager) handleBindingDelete(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.bindings[entry.ID]; !exists {
		return fmt.Errorf("contract binding '%s' not found for deletion", entry.ID)
	}

	delete(m.bindings, entry.ID)

	m.bus.Send(ctx, event.Event{
		System: contract.System,
		Kind:   contract.DeleteBinding,
		Path:   entry.ID.String(),
	})

	m.log.Info("contract binding deleted", zap.String("id", entry.ID.String()))
	return nil
}

// --- Helper methods for decoding ---

func (m *Manager) decodeDefinition(entry registry.Entry) (*apidi.DefinitionConfig, error) {
	if entry.Data == nil {
		return nil, fmt.Errorf("definition data is required for entry '%s'", entry.ID)
	}
	cfg := &apidi.DefinitionConfig{Meta: entry.Meta}
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal definition config for entry '%s': %w", entry.ID, err)
	}
	return cfg, nil
}

func (m *Manager) decodeBinding(entry registry.Entry) (*apidi.BindingConfig, error) {
	if entry.Data == nil {
		return nil, fmt.Errorf("binding data is required for entry '%s'", entry.ID)
	}
	cfg := &apidi.BindingConfig{Meta: entry.Meta}
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal binding config for entry '%s': %w", entry.ID, err)
	}
	return cfg, nil
}
