// SPDX-License-Identifier: MPL-2.0

package di

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/wippyai/runtime/api/contract"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	apidi "github.com/wippyai/runtime/api/service/di"
	entryutil "github.com/wippyai/runtime/system/entry"
	"go.uber.org/zap"
)

// Manager handles contract registry entries and forwards them to the contract system plane
type Manager struct {
	log *zap.Logger
	dtt payload.Transcoder
	bus event.Bus

	definitions map[registry.ID]*contract.Definition
	bindings    map[registry.ID]*contract.Binding
	tx          *txState
	mu          sync.RWMutex
}

// Manager participates in registry transactions so that a changeset updating a
// contract definition together with its bindings can be validated as a whole.
var _ registry.TransactionListener = (*Manager)(nil)

// txState stages contract mutations for the duration of one registry
// transaction. Entries are validated structurally as they arrive, but
// cross-entry validation (binding completeness against definitions, unique
// defaults, definition-in-use) is deferred to Commit, when the complete
// post-transaction state is known. That is what makes a definition update and
// its binding updates applicable in one changeset: neither half is valid
// against the pre-change peer, but the pair is valid together.
type txState struct {
	definitions        map[registry.ID]*contract.Definition
	bindings           map[registry.ID]*contract.Binding
	deletedDefinitions map[registry.ID]struct{}
	deletedBindings    map[registry.ID]struct{}
	events             []event.Event
}

func newTxState() *txState {
	return &txState{
		definitions:        make(map[registry.ID]*contract.Definition),
		bindings:           make(map[registry.ID]*contract.Binding),
		deletedDefinitions: make(map[registry.ID]struct{}),
		deletedBindings:    make(map[registry.ID]struct{}),
	}
}

func (tx *txState) stageDefinition(id registry.ID, def *contract.Definition, evt event.Event) {
	tx.definitions[id] = def
	delete(tx.deletedDefinitions, id)
	tx.events = append(tx.events, evt)
}

func (tx *txState) deleteDefinition(id registry.ID, evt event.Event) {
	delete(tx.definitions, id)
	tx.deletedDefinitions[id] = struct{}{}
	tx.events = append(tx.events, evt)
}

func (tx *txState) stageBinding(id registry.ID, binding *contract.Binding, evt event.Event) {
	tx.bindings[id] = binding
	delete(tx.deletedBindings, id)
	tx.events = append(tx.events, evt)
}

func (tx *txState) deleteBinding(id registry.ID, evt event.Event) {
	delete(tx.bindings, id)
	tx.deletedBindings[id] = struct{}{}
	tx.events = append(tx.events, evt)
}

func (tx *txState) effectiveDefinitions(base map[registry.ID]*contract.Definition) map[registry.ID]*contract.Definition {
	out := make(map[registry.ID]*contract.Definition, len(base)+len(tx.definitions))
	for id, def := range base {
		if _, gone := tx.deletedDefinitions[id]; !gone {
			out[id] = def
		}
	}
	for id, def := range tx.definitions {
		out[id] = def
	}
	return out
}

func (tx *txState) effectiveBindings(base map[registry.ID]*contract.Binding) map[registry.ID]*contract.Binding {
	out := make(map[registry.ID]*contract.Binding, len(base)+len(tx.bindings))
	for id, binding := range base {
		if _, gone := tx.deletedBindings[id]; !gone {
			out[id] = binding
		}
	}
	for id, binding := range tx.bindings {
		out[id] = binding
	}
	return out
}

// NewManager creates a new contract manager
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *Manager {
	if log == nil {
		log = zap.NewNop()
	}
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
	case apidi.Definition:
		return m.handleDefinitionAdd(ctx, entry)
	case apidi.Binding:
		return m.handleBindingAdd(ctx, entry)
	default:
		return NewUnsupportedEntryKindError(entry.Kind)
	}
}

// Update handles updates to existing contract definitions and bindings
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case apidi.Definition:
		return m.handleDefinitionUpdate(ctx, entry)
	case apidi.Binding:
		return m.handleBindingUpdate(ctx, entry)
	default:
		return NewUnsupportedEntryKindError(entry.Kind)
	}
}

// Delete handles removal of contract definitions and bindings
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case apidi.Definition:
		return m.handleDefinitionDelete(ctx, entry)
	case apidi.Binding:
		return m.handleBindingDelete(ctx, entry)
	default:
		return NewUnsupportedEntryKindError(entry.Kind)
	}
}

// --- registry.TransactionListener ---

// Begin opens a staging area. Until Commit, entry operations are validated
// structurally, staged, and their contract-plane events queued.
func (m *Manager) Begin(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tx = newTxState()
	return nil
}

// Commit validates the complete staged state and applies it atomically. On a
// validation failure nothing is applied or emitted, and staging remains live
// while the registry runner dispatches inverse operations before Discard.
func (m *Manager) Commit(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx := m.tx
	if tx == nil {
		return nil
	}
	definitions := tx.effectiveDefinitions(m.definitions)
	bindings := tx.effectiveBindings(m.bindings)
	for bindingID, binding := range bindings {
		if err := validateBindingAgainstDefinitions(binding, bindingID, definitions); err != nil {
			return err
		}
		if err := validateUniqueDefaults(binding, bindingID, bindings); err != nil {
			return err
		}
	}
	m.definitions = definitions
	m.bindings = bindings
	events := tx.events
	m.tx = nil
	for _, evt := range events {
		m.bus.Send(ctx, evt)
	}
	if len(events) > 0 {
		m.log.Info("contract transaction committed",
			zap.Int("staged_events", len(events)),
			zap.Int("definitions", len(definitions)),
			zap.Int("bindings", len(bindings)))
	}
	return nil
}

// Discard drops the staged state without applying it. This is also the final
// step after a failed Commit and the runner's staged rollback operations.
func (m *Manager) Discard(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tx = nil
	return nil
}

// definitionInEffect reports whether a definition exists in the state this
// transaction would produce; outside a transaction it reads committed state.
// Callers hold m.mu.
func (m *Manager) definitionInEffect(id registry.ID) bool {
	if m.tx != nil {
		if _, staged := m.tx.definitions[id]; staged {
			return true
		}
		if _, gone := m.tx.deletedDefinitions[id]; gone {
			return false
		}
	}
	_, exists := m.definitions[id]
	return exists
}

// bindingInEffect mirrors definitionInEffect for bindings. Callers hold m.mu.
func (m *Manager) bindingInEffect(id registry.ID) bool {
	if m.tx != nil {
		if _, staged := m.tx.bindings[id]; staged {
			return true
		}
		if _, gone := m.tx.deletedBindings[id]; gone {
			return false
		}
	}
	_, exists := m.bindings[id]
	return exists
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
			return NewMethodNameEmptyError(defID)
		}
		if _, exists := methodNames[method.Name]; exists {
			return NewDuplicateMethodNameError(method.Name, defID)
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
				return NewInputSchemaMissingFormatError(i, method.Name, defID)
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
				return NewOutputSchemaMissingFormatError(i, method.Name, defID)
			}
		}
	}
	return nil
}

// validateBindingAgainstDefinitions checks if a binding is valid with the current set of definitions.
// Assumes m.mu is RLock'd or Lock'd by the caller appropriately for m.definitions access.
func validateBindingAgainstDefinitions(binding *contract.Binding, bindingID registry.ID, definitions map[registry.ID]*contract.Definition) error {
	if len(binding.Contracts) == 0 {
		return NewBindingNoContractsError(bindingID)
	}
	for i, bc := range binding.Contracts {
		contractDef, exists := definitions[bc.Contract]
		if !exists {
			return NewContractNotFoundError(bindingID, i, bc.Contract)
		}

		// Check method completeness: all methods in definition must be bound.
		defMethodNames := make(map[string]struct{})
		for _, methodDef := range contractDef.Methods {
			defMethodNames[methodDef.Name] = struct{}{}
			if _, bound := bc.Methods[methodDef.Name]; !bound {
				return NewMethodNotBoundError(bindingID, bc.Contract, methodDef.Name)
			}
		}

		// Check for extraneous methods: all bound methods must exist in definition.
		for methodName := range bc.Methods {
			if _, defined := defMethodNames[methodName]; !defined {
				return NewMethodNotDefinedError(bindingID, bc.Contract, methodName)
			}
		}
	}
	return nil
}

// validateUniqueDefaults checks that no contract has multiple default bindings
// This ensures that each contract can have at most one default binding, preventing conflicts
// when using default binding resolution (contract:open() without binding ID)
func validateUniqueDefaults(binding *contract.Binding, bindingID registry.ID, bindings map[registry.ID]*contract.Binding) error {
	for _, bc := range binding.Contracts {
		if bc.Default {
			// Check if another binding already has default for this contract
			for otherBindingID, otherBinding := range bindings {
				if otherBindingID == bindingID {
					continue // Skip self
				}
				for _, otherBC := range otherBinding.Contracts {
					if otherBC.Contract == bc.Contract && otherBC.Default {
						return NewDuplicateDefaultBindingError(bc.Contract, otherBindingID, bindingID)
					}
				}
			}
		}
	}
	return nil
}

// --- Contract Definition handlers ---

func (m *Manager) handleDefinitionAdd(ctx context.Context, entry registry.Entry) error {
	cfg, err := entryutil.DecodeEntryConfig[apidi.DefinitionConfig](ctx, m.dtt, entry)
	if err != nil {
		return NewDecodeDefinitionError(entry.ID, err)
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

	if m.definitionInEffect(entry.ID) {
		return NewDefinitionAlreadyExistsError(entry.ID)
	}

	registerEvent := event.Event{
		System: contract.System,
		Kind:   contract.RegisterDefinition,
		Path:   entry.ID.String(),
		Data:   definition,
	}
	if m.tx != nil {
		m.tx.stageDefinition(entry.ID, definition, registerEvent)
		return nil
	}

	m.definitions[entry.ID] = definition
	m.bus.Send(ctx, registerEvent)

	m.log.Debug("contract definition registered",
		zap.String("id", entry.ID.String()),
		zap.Int("methods", len(definition.Methods)))
	return nil
}

func (m *Manager) handleDefinitionUpdate(ctx context.Context, entry registry.Entry) error {
	cfg, err := entryutil.DecodeEntryConfig[apidi.DefinitionConfig](ctx, m.dtt, entry)
	if err != nil {
		return NewDecodeDefinitionUpdateError(entry.ID, err)
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

	if !m.definitionInEffect(entry.ID) {
		return NewDefinitionNotFoundForUpdateError(entry.ID)
	}

	if m.tx != nil {
		// Cross-binding validation is deferred to Commit: the binding updates
		// this definition change pairs with may arrive later in the same
		// transaction, and against the pre-change bindings the update is not
		// yet valid.
		m.tx.stageDefinition(entry.ID, updatedDefinition, event.Event{
			System: contract.System,
			Kind:   contract.UpdateDefinition,
			Path:   entry.ID.String(),
			Data:   updatedDefinition,
		})
		return nil
	}

	originalDefinition := m.definitions[entry.ID]

	// Temporarily apply the update to check dependent bindings
	m.definitions[entry.ID] = updatedDefinition
	var validationError error
	for bindingID, binding := range m.bindings {
		usesUpdatedDef := false
		for _, boundContract := range binding.Contracts {
			if boundContract.Contract.Equal(entry.ID) {
				usesUpdatedDef = true
				break
			}
		}
		if usesUpdatedDef {
			// Re-validate this binding against the *new* definition
			if err := validateBindingAgainstDefinitions(binding, bindingID, m.definitions); err != nil {
				validationError = NewUpdateWouldInvalidateBindingError(entry.ID, bindingID, err)
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

	if !m.definitionInEffect(entry.ID) {
		return NewDefinitionNotFoundForDeleteError(entry.ID)
	}

	if m.tx != nil {
		// The in-use check is deferred to Commit: a binding referencing this
		// definition may be deleted later in the same transaction, and Commit's
		// full validation reports any survivor as an unresolved contract.
		m.tx.deleteDefinition(entry.ID, event.Event{
			System: contract.System,
			Kind:   contract.DeleteDefinition,
			Path:   entry.ID.String(),
		})
		return nil
	}

	// Check if any binding refers to this definition
	for bindingID, binding := range m.bindings {
		for _, boundContract := range binding.Contracts {
			if boundContract.Contract.Equal(entry.ID) {
				return NewDefinitionInUseError(entry.ID, bindingID)
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
	cfg, err := entryutil.DecodeEntryConfig[apidi.BindingConfig](ctx, m.dtt, entry)
	if err != nil {
		return NewDecodeBindingError(entry.ID, err)
	}
	binding := cfg.ToBinding()

	// Set ID and Meta from entry
	binding.ID = entry.ID
	binding.Meta = entry.Meta

	m.mu.Lock() // Lock for m.bindings write and m.definitions read
	defer m.mu.Unlock()

	if m.bindingInEffect(entry.ID) {
		return NewBindingAlreadyExistsError(entry.ID)
	}

	if m.tx != nil {
		// Validation against definitions is deferred to Commit, where the
		// definitions this binding depends on are guaranteed staged.
		m.tx.stageBinding(entry.ID, binding, event.Event{
			System: contract.System,
			Kind:   contract.RegisterBinding,
			Path:   entry.ID.String(),
			Data:   binding,
		})
		return nil
	}

	// validateBindingAgainstDefinitions needs read access to m.definitions, which is covered by the Lock
	if err := validateBindingAgainstDefinitions(binding, entry.ID, m.definitions); err != nil {
		return err // Error from validateBinding already includes bindingID
	}

	// Validate unique defaults - needs read access to m.bindings, which is covered by the Lock
	// This prevents multiple bindings from being marked as default for the same contract
	if err := validateUniqueDefaults(binding, entry.ID, m.bindings); err != nil {
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
	cfg, err := entryutil.DecodeEntryConfig[apidi.BindingConfig](ctx, m.dtt, entry)
	if err != nil {
		return NewDecodeBindingUpdateError(entry.ID, err)
	}
	updatedBinding := cfg.ToBinding()

	// Set ID and Meta from entry
	updatedBinding.ID = entry.ID
	updatedBinding.Meta = entry.Meta

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.bindingInEffect(entry.ID) {
		return NewBindingNotFoundForUpdateError(entry.ID)
	}

	if m.tx != nil {
		m.tx.stageBinding(entry.ID, updatedBinding, event.Event{
			System: contract.System,
			Kind:   contract.UpdateBinding,
			Path:   entry.ID.String(),
			Data:   updatedBinding,
		})
		return nil
	}

	if err := validateBindingAgainstDefinitions(updatedBinding, entry.ID, m.definitions); err != nil {
		return err // Error from validateBinding already includes bindingID
	}

	// Validate unique defaults for the updated binding
	// This ensures that updating a binding to set default=true doesn't conflict with existing defaults
	if err := validateUniqueDefaults(updatedBinding, entry.ID, m.bindings); err != nil {
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

	if !m.bindingInEffect(entry.ID) {
		return NewBindingNotFoundForDeleteError(entry.ID)
	}

	if m.tx != nil {
		m.tx.deleteBinding(entry.ID, event.Event{
			System: contract.System,
			Kind:   contract.DeleteBinding,
			Path:   entry.ID.String(),
		})
		return nil
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
