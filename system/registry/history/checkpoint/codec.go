// SPDX-License-Identifier: MPL-2.0

package checkpoint

import (
	"fmt"
	"sort"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/wippyai/runtime/api/registry"
)

type stateWire struct {
	Operations            []byte
	Tombstones            []registry.ID
	Dependencies          []rangeWire
	AmbiguousDeletes      []rangeWire
	Selections            []selectionWire
	Entries               int
	Transaction, Sequence uint64
}

type positionWire struct {
	Index                 int
	Transaction, Sequence uint64
}
type rangeWire struct {
	ID          registry.ID
	First, Last positionWire
}
type selectionWire struct {
	Component string
	Position  positionWire
}

// StateCodec uses the backend's existing changeset payload encoding for every
// retained entry, including legacy originals. The surrounding format is shared.
type StateCodec struct {
	Encode func(registry.ChangeSet) ([]byte, error)
	Decode func([]byte) (registry.ChangeSet, error)
}

func sortedIDs[T any](values map[registry.ID]T) []registry.ID {
	ids := make([]registry.ID, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	return ids
}

func (c StateCodec) EncodeState(state *registry.ReplayState) ([]byte, error) {
	wire := stateWire{Transaction: state.Transaction, Sequence: state.Sequence}
	var operations registry.ChangeSet
	for _, id := range sortedIDs(state.Entries) {
		operations = append(operations, registry.Operation{Kind: registry.EntryCreate, Entry: state.Entries[id]})
	}
	wire.Entries = len(operations)
	wire.Tombstones = sortedIDs(state.Tombstones)
	position := func(op registry.ReplayOperation) positionWire {
		p := positionWire{Index: len(operations), Transaction: op.Transaction, Sequence: op.Sequence}
		operations = append(operations, op.Operation)
		return p
	}
	ranges := func(values map[registry.ID]registry.ReplayOperationRange) []rangeWire {
		result := make([]rangeWire, 0, len(values))
		for _, id := range sortedIDs(values) {
			v := values[id]
			result = append(result, rangeWire{ID: id, First: position(v.First), Last: position(v.Last)})
		}
		return result
	}
	wire.Dependencies = ranges(state.Dependencies)
	wire.AmbiguousDeletes = ranges(state.AmbiguousDeletes)
	components := make([]string, 0, len(state.Selections))
	for component := range state.Selections {
		components = append(components, component)
	}
	sort.Strings(components)
	for _, component := range components {
		wire.Selections = append(wire.Selections, selectionWire{Component: component, Position: position(state.Selections[component])})
	}
	data, err := c.Encode(operations)
	if err != nil {
		return nil, err
	}
	wire.Operations = data
	handle := &codec.MsgpackHandle{}
	handle.Canonical = true
	var encoded []byte
	if err := codec.NewEncoderBytes(&encoded, handle).Encode(wire); err != nil {
		return nil, err
	}
	return encoded, nil
}

func (c StateCodec) DecodeState(data []byte) (*registry.ReplayState, error) {
	var wire stateWire
	handle := &codec.MsgpackHandle{}
	handle.ErrorIfNoField = true
	decoder := codec.NewDecoderBytes(data, handle)
	if err := decoder.Decode(&wire); err != nil {
		return nil, err
	}
	if decoder.NumBytesRead() != len(data) {
		return nil, fmt.Errorf("replay state has trailing bytes")
	}
	operations, err := c.Decode(wire.Operations)
	if err != nil {
		return nil, err
	}
	if wire.Entries < 0 || wire.Entries > len(operations) {
		return nil, fmt.Errorf("replay state has invalid entry count")
	}
	state := registry.NewReplayState()
	state.Transaction = wire.Transaction
	state.Sequence = wire.Sequence
	for _, op := range operations[:wire.Entries] {
		if op.Kind != registry.EntryCreate {
			return nil, fmt.Errorf("replay state has invalid overlay operation")
		}
		id := op.Entry.ID.Canonical()
		if _, exists := state.Entries[id]; exists {
			return nil, fmt.Errorf("replay state has duplicate overlay ID")
		}
		op.Entry.ID = id
		state.Entries[id] = op.Entry
	}
	for _, id := range wire.Tombstones {
		id = id.Canonical()
		if _, exists := state.Entries[id]; exists {
			return nil, fmt.Errorf("replay state entry is also tombstoned")
		}
		if _, exists := state.Tombstones[id]; exists {
			return nil, fmt.Errorf("replay state has duplicate tombstone")
		}
		state.Tombstones[id] = struct{}{}
	}
	used := make(map[int]bool)
	readPosition := func(p positionWire) (registry.ReplayOperation, error) {
		if p.Index < wire.Entries || p.Index >= len(operations) || used[p.Index] || p.Transaction == 0 || p.Transaction > wire.Transaction || p.Sequence == 0 || p.Sequence > wire.Sequence {
			return registry.ReplayOperation{}, fmt.Errorf("replay state has invalid operation position")
		}
		used[p.Index] = true
		op := operations[p.Index]
		op.Entry.ID = op.Entry.ID.Canonical()
		if op.OriginalEntry != nil {
			original := *op.OriginalEntry
			original.ID = original.ID.Canonical()
			op.OriginalEntry = &original
		}
		return registry.ReplayOperation{Operation: op, Transaction: p.Transaction, Sequence: p.Sequence}, nil
	}
	readRanges := func(wires []rangeWire, dst map[registry.ID]registry.ReplayOperationRange) error {
		for _, r := range wires {
			id := r.ID.Canonical()
			if _, exists := dst[id]; exists {
				return fmt.Errorf("replay state has duplicate dependency range")
			}
			first, err := readPosition(r.First)
			if err != nil {
				return err
			}
			last, err := readPosition(r.Last)
			if err != nil {
				return err
			}
			if !first.Entry.ID.Equal(id) || !last.Entry.ID.Equal(id) || first.Sequence > last.Sequence || first.Transaction > last.Transaction {
				return fmt.Errorf("replay state has invalid dependency range")
			}
			dst[id] = registry.ReplayOperationRange{First: first, Last: last}
		}
		return nil
	}
	if err := readRanges(wire.Dependencies, state.Dependencies); err != nil {
		return nil, err
	}
	if err := readRanges(wire.AmbiguousDeletes, state.AmbiguousDeletes); err != nil {
		return nil, err
	}
	for _, selection := range wire.Selections {
		if selection.Component == "" {
			return nil, fmt.Errorf("replay state has empty component selection")
		}
		if _, exists := state.Selections[selection.Component]; exists {
			return nil, fmt.Errorf("replay state has duplicate component selection")
		}
		op, err := readPosition(selection.Position)
		if err != nil {
			return nil, err
		}
		if op.Entry.Kind != registry.NamespaceDependency {
			return nil, fmt.Errorf("replay state has invalid component selection")
		}
		state.Selections[selection.Component] = op
	}
	if len(used) != len(operations)-wire.Entries {
		return nil, fmt.Errorf("replay state has unreferenced operations")
	}
	return state, nil
}
