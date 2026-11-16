// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/wippyai/runtime/api/payload"
)

// DependencyDeclaration is the part of a dependency entry that identifies a
// selected component. The dependency resolver owns the remaining fields.
type DependencyDeclaration struct {
	Component string `json:"component"`
}

// ReplayOperation identifies an operation in the replay transaction that
// supplied it. Sequence preserves order inside one transaction.
type ReplayOperation struct {
	Operation
	Transaction uint64 `json:"transaction"`
	Sequence    uint64 `json:"sequence"`
}

// ReplayOperationRange retains the first legacy-input evidence and the final
// effective operation without retaining intermediate history.
type ReplayOperationRange struct {
	First ReplayOperation `json:"first"`
	Last  ReplayOperation `json:"last"`
}

// ReplayState is the compact history projection used for registry replay.
// Entries and Tombstones form the authored overlay. Dependency operations keep
// the evidence needed to restore module-owned declarations after a selection.
type ReplayState struct {
	Entries          map[ID]Entry                `json:"entries"`
	Tombstones       map[ID]struct{}             `json:"tombstones"`
	Dependencies     map[ID]ReplayOperationRange `json:"dependencies"`
	AmbiguousDeletes map[ID]ReplayOperationRange `json:"ambiguous_deletes"`
	Selections       map[string]ReplayOperation  `json:"selections"`
	Transaction      uint64                      `json:"transaction"`
	Sequence         uint64                      `json:"sequence"`
}

// NewReplayState returns an empty replay projection.
func NewReplayState() *ReplayState {
	return &ReplayState{
		Entries:          make(map[ID]Entry),
		Tombstones:       make(map[ID]struct{}),
		Dependencies:     make(map[ID]ReplayOperationRange),
		AmbiguousDeletes: make(map[ID]ReplayOperationRange),
		Selections:       make(map[string]ReplayOperation),
	}
}

// Apply folds one persisted changeset into the replay projection.
func (s *ReplayState) Apply(ctx context.Context, changes ChangeSet) error {
	return s.ApplyWithTranscoder(changes, payload.GetTranscoder(ctx))
}

// ApplyWithTranscoder folds one persisted changeset using the payload decoder
// supplied by the history owner. A nil decoder supports native Golang payloads
// only; encoded payloads require their registered decoder.
func (s *ReplayState) ApplyWithTranscoder(changes ChangeSet, transcoder payload.Transcoder) error {
	return s.apply(changes, transcoder, false)
}

// ApplyDependencyChanges folds dependency-relevant legacy transactions through
// the same reducer. WithDependencyBaseline callers already provide this
// filtered partition, including kind replacements that have no original entry.
func (s *ReplayState) ApplyDependencyChanges(changes ChangeSet, transcoder payload.Transcoder) error {
	return s.apply(changes, transcoder, true)
}

func (s *ReplayState) apply(changes ChangeSet, transcoder payload.Transcoder, dependencyChanges bool) error {
	if s == nil {
		return fmt.Errorf("registry replay state is nil")
	}
	s.ensureMaps()
	s.Transaction++
	for _, operation := range changes {
		s.Sequence++
		recorded := ReplayOperation{Operation: operation, Transaction: s.Transaction, Sequence: s.Sequence}
		id := operation.Entry.ID.Canonical()
		operation.Entry.ID = id
		if operation.OriginalEntry != nil {
			original := *operation.OriginalEntry
			original.ID = original.ID.Canonical()
			operation.OriginalEntry = &original
		}
		prior, present := s.Entries[id]
		_, tombstoned := s.Tombstones[id]
		if operation.Kind == EntryDelete {
			if operation.OriginalEntry != nil {
				operation.Entry = *operation.OriginalEntry
			} else if present {
				operation.Entry = prior
			}
		}
		recorded.Operation = operation
		relevant, ambiguous := dependencyRelevant(operation, prior, present)
		if tombstoned && operation.Kind == EntryDelete && operation.OriginalEntry == nil {
			ambiguous = false
		}
		if dependencyChanges {
			relevant, ambiguous = true, false
		}
		if relevant {
			recordRange(s.Dependencies, id, recorded)
		} else if ambiguous {
			recordRange(s.AmbiguousDeletes, id, recorded)
		}
		if operation.Entry.Kind == NamespaceDependency {
			component, selected, err := dependencyComponent(operation.Entry, transcoder)
			if err != nil {
				return err
			}
			if selected {
				s.Selections[component] = recorded
			}
		}

		switch operation.Kind {
		case EntryCreate, EntryUpdate:
			s.Entries[id] = operation.Entry
			delete(s.Tombstones, id)
		case EntryDelete:
			delete(s.Entries, id)
			s.Tombstones[id] = struct{}{}
		}
	}
	return nil
}

// DependencySelection returns the final selection of component.
func (s *ReplayState) DependencySelection(component string) (ReplayOperation, bool) {
	if s == nil {
		return ReplayOperation{}, false
	}
	op, ok := s.Selections[component]
	return op, ok
}

// DependencySelectionWithBaseline includes compact deletes whose dependency
// declaration comes from the immutable baseline.
func (s *ReplayState) DependencySelectionWithBaseline(component string, baseline State, transcoder payload.Transcoder) (ReplayOperation, bool, error) {
	selections, err := s.DependencySelectionsWithBaseline(baseline, transcoder)
	if err != nil {
		return ReplayOperation{}, false, err
	}
	selection, selected := selections[component]
	return selection, selected, nil
}

// DependencySelectionsWithBaseline returns final component selections,
// including compact deletes whose declarations come from the baseline.
func (s *ReplayState) DependencySelectionsWithBaseline(baseline State, transcoder payload.Transcoder) (map[string]ReplayOperation, error) {
	selections := make(map[string]ReplayOperation)
	if s == nil {
		return selections, nil
	}
	for component, selection := range s.Selections {
		selections[component] = selection
	}
	dependencies := baselineDependencyIndex(baseline)
	for id, evidence := range s.AmbiguousDeletes {
		entry, dependency := dependencies[id.Canonical()]
		if !dependency {
			continue
		}
		selectedComponent, isSelection, err := dependencyComponent(entry, transcoder)
		if err != nil {
			return nil, err
		}
		if !isSelection {
			continue
		}
		selection, selected := selections[selectedComponent]
		if !selected || evidence.Last.Sequence > selection.Sequence {
			selections[selectedComponent] = evidence.Last
		}
	}
	return selections, nil
}

// FirstDependencyOperation returns first dependency-relevant evidence for id.
// A kindless delete becomes dependency-relevant when baseline supplies a
// dependency entry with the same ID.
func (s *ReplayState) FirstDependencyOperation(id ID, baseline State) (Operation, bool) {
	evidence, ok := s.dependencyRange(id, baseline)
	if !ok {
		return Operation{}, false
	}
	return evidence.First.Operation, true
}

// LastDependencyOperation returns the final dependency-relevant operation for
// id. A kindless delete is resolved against the immutable baseline.
func (s *ReplayState) LastDependencyOperation(id ID, baseline State) (ReplayOperation, bool) {
	evidence, ok := s.dependencyRange(id, baseline)
	if !ok {
		return ReplayOperation{}, false
	}
	return evidence.Last, true
}

// LastDependencyOperations returns one compact effective operation per
// dependency-relevant entry ID.
func (s *ReplayState) LastDependencyOperations(baseline State) map[ID]ReplayOperation {
	if s == nil {
		return nil
	}
	operations := make(map[ID]ReplayOperation, len(s.Dependencies)+len(s.AmbiguousDeletes))
	dependencies := baselineDependencyIndex(baseline)
	for id := range s.Dependencies {
		if operation, ok := s.lastDependencyOperation(id, dependencies); ok {
			operations[id] = operation
		}
	}
	for id := range s.AmbiguousDeletes {
		if operation, ok := s.lastDependencyOperation(id, dependencies); ok {
			operations[id] = operation
		}
	}
	return operations
}

// FirstDependencyOperations returns the compact first-operation evidence for
// every dependency-relevant entry ID.
func (s *ReplayState) FirstDependencyOperations(baseline State) map[ID]Operation {
	if s == nil {
		return nil
	}
	operations := make(map[ID]Operation, len(s.Dependencies)+len(s.AmbiguousDeletes))
	dependencies := baselineDependencyIndex(baseline)
	for id := range s.Dependencies {
		if operation, ok := s.firstDependencyOperation(id, dependencies); ok {
			operations[id] = operation
		}
	}
	for id := range s.AmbiguousDeletes {
		if operation, ok := s.firstDependencyOperation(id, dependencies); ok {
			operations[id] = operation
		}
	}
	return operations
}

// IsTombstoned reports whether authored replay deleted id.
func (s *ReplayState) IsTombstoned(id ID) bool {
	if s == nil {
		return false
	}
	_, ok := s.Tombstones[id.Canonical()]
	return ok
}

func (s *ReplayState) dependencyRange(id ID, baseline State) (ReplayOperationRange, bool) {
	return s.dependencyRangeWithIndex(id, baselineDependencyIndex(baseline))
}

func (s *ReplayState) lastDependencyOperation(id ID, dependencies map[ID]Entry) (ReplayOperation, bool) {
	evidence, ok := s.dependencyRangeWithIndex(id, dependencies)
	if !ok {
		return ReplayOperation{}, false
	}
	return evidence.Last, true
}

func (s *ReplayState) firstDependencyOperation(id ID, dependencies map[ID]Entry) (Operation, bool) {
	evidence, ok := s.dependencyRangeWithIndex(id, dependencies)
	if !ok {
		return Operation{}, false
	}
	return evidence.First.Operation, true
}

func (s *ReplayState) dependencyRangeWithIndex(id ID, dependencies map[ID]Entry) (ReplayOperationRange, bool) {
	if s == nil {
		return ReplayOperationRange{}, false
	}
	id = id.Canonical()
	known, knownOK := s.Dependencies[id]
	ambiguous, ambiguousOK := s.AmbiguousDeletes[id]
	_, baselineKnown := dependencies[id]
	if !ambiguousOK || !baselineKnown {
		if !knownOK {
			return ReplayOperationRange{}, false
		}
		return normalizeDependencyRange(known, dependencies), true
	}
	if !knownOK {
		return normalizeDependencyRange(ambiguous, dependencies), true
	}
	if ambiguous.First.Sequence < known.First.Sequence {
		known.First = ambiguous.First
	}
	if ambiguous.Last.Sequence > known.Last.Sequence {
		known.Last = ambiguous.Last
	}
	return normalizeDependencyRange(known, dependencies), true
}

func (s *ReplayState) ensureMaps() {
	if s.Entries == nil {
		s.Entries = make(map[ID]Entry)
	}
	if s.Tombstones == nil {
		s.Tombstones = make(map[ID]struct{})
	}
	if s.Dependencies == nil {
		s.Dependencies = make(map[ID]ReplayOperationRange)
	}
	if s.AmbiguousDeletes == nil {
		s.AmbiguousDeletes = make(map[ID]ReplayOperationRange)
	}
	if s.Selections == nil {
		s.Selections = make(map[string]ReplayOperation)
	}
}

func recordRange(dst map[ID]ReplayOperationRange, id ID, operation ReplayOperation) {
	evidence, ok := dst[id]
	if !ok {
		dst[id] = ReplayOperationRange{First: operation, Last: operation}
		return
	}
	evidence.Last = operation
	dst[id] = evidence
}

func dependencyRelevant(operation Operation, prior Entry, priorPresent bool) (known, ambiguous bool) {
	if operation.Entry.Kind == NamespaceDependency || (operation.OriginalEntry != nil && operation.OriginalEntry.Kind == NamespaceDependency) {
		return true, false
	}
	if operation.Kind == EntryDelete && priorPresent && prior.Kind == NamespaceDependency {
		return true, false
	}
	return false, operation.Kind == EntryDelete && operation.Entry.Kind == "" && operation.OriginalEntry == nil
}

func baselineDependencyIndex(baseline State) map[ID]Entry {
	dependencies := make(map[ID]Entry)
	for _, entry := range baseline {
		if entry.Kind == NamespaceDependency {
			dependencies[entry.ID.Canonical()] = entry
		}
	}
	return dependencies
}

func normalizeDependencyRange(evidence ReplayOperationRange, dependencies map[ID]Entry) ReplayOperationRange {
	entry, ok := dependencies[evidence.Last.Entry.ID.Canonical()]
	if !ok {
		return evidence
	}
	if evidence.First.Entry.Kind == "" {
		evidence.First.Entry = entry
	}
	if evidence.Last.Entry.Kind == "" {
		evidence.Last.Entry = entry
	}
	return evidence
}

func dependencyComponent(entry Entry, transcoder payload.Transcoder) (string, bool, error) {
	if entry.Data == nil {
		return "", false, nil
	}
	if entry.Data.Format() == payload.String {
		return "", false, nil
	}
	data := entry.Data.Data()
	if entry.Data.Format() != payload.Golang {
		if transcoder == nil {
			return "", false, fmt.Errorf("decode dependency %s: payload transcoder is required", entry.ID.String())
		}
		decoded, err := transcoder.Transcode(entry.Data, payload.Golang)
		if err != nil {
			return "", false, fmt.Errorf("decode dependency %s: %w", entry.ID.String(), err)
		}
		data = decoded.Data()
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", false, fmt.Errorf("encode dependency %s: %w", entry.ID.String(), err)
	}
	if len(encoded) == 0 || encoded[0] != '{' {
		return "", false, nil
	}
	var declaration DependencyDeclaration
	if err := json.Unmarshal(encoded, &declaration); err != nil {
		return "", false, fmt.Errorf("decode dependency %s: %w", entry.ID.String(), err)
	}
	if declaration.Component == "" {
		return "", false, nil
	}
	return declaration.Component, true, nil
}
