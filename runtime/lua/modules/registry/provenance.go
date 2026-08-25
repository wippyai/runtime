// SPDX-License-Identifier: MPL-2.0

package registry

import (
	lua "github.com/wippyai/go-lua"
	regapi "github.com/wippyai/runtime/api/registry"
)

// provenanceReader answers provenance questions about the live registry.
type provenanceReader interface {
	EntryProvenance(regapi.ID) (regapi.EntryProvenance, bool)
}

// registryProvenance returns the provenance record for one entry of the live
// state: {module, version, digest, root}, or nil for an unknown entry. Values
// are copies; mutating the table changes nothing.
func registryProvenance(l *lua.LState) int {
	idStr := l.CheckString(1)

	reg := regapi.GetRegistry(l.Context())
	if reg == nil {
		err := lua.NewLuaError(l, "registry not available").
			WithKind(lua.Unavailable).
			WithRetryable(true)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	reader, ok := reg.(provenanceReader)
	if !ok {
		err := lua.NewLuaError(l, "registry does not serve provenance").
			WithKind(lua.Unavailable).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	p, found := reader.EntryProvenance(regapi.ParseID(idStr).Canonical())
	if !found {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	table := l.CreateTable(0, 4)
	table.RawSetString("module", lua.LString(p.Module))
	table.RawSetString("version", lua.LString(p.Version))
	table.RawSetString("digest", lua.LString(p.Digest))
	table.RawSetString("root", lua.LBool(p.Root))
	l.Push(table)
	l.Push(lua.LNil)
	return 2
}

// registrySetRoot flips the deployment-root selection of one ns.dependency
// entry. Root-ness is deployment state held by the registry, not entry
// content: the operation carries the entry unchanged and the flipped flag in
// its provenance, so no author payload moves.
func registrySetRoot(l *lua.LState) int {
	idStr := l.CheckString(1)
	enable := l.CheckBool(2)

	reg := regapi.GetRegistry(l.Context())
	if reg == nil {
		err := lua.NewLuaError(l, "registry not available").
			WithKind(lua.Unavailable).
			WithRetryable(true)
		l.Push(lua.LFalse)
		l.Push(err)
		return 2
	}

	id := regapi.ParseID(idStr).Canonical()
	entry, getErr := reg.GetEntry(id)
	if getErr != nil {
		err := lua.NewLuaError(l, "entry not found: "+id.String()).
			WithKind(lua.NotFound).
			WithRetryable(false)
		l.Push(lua.LFalse)
		l.Push(err)
		return 2
	}
	if entry.Kind != regapi.NamespaceDependency {
		err := lua.NewLuaError(l, "only ns.dependency entries select deployment roots").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LFalse)
		l.Push(err)
		return 2
	}

	var current regapi.EntryProvenance
	if reader, ok := reg.(provenanceReader); ok {
		current, _ = reader.EntryProvenance(id)
	}
	if current.Root == enable {
		l.Push(lua.LTrue)
		l.Push(lua.LNil)
		return 2
	}

	next := current
	next.Root = enable
	if _, applyErr := reg.Apply(l.Context(), regapi.ChangeSet{{
		Kind:       regapi.EntryUpdate,
		Entry:      entry,
		Provenance: &next,
	}}); applyErr != nil {
		err := lua.WrapErrorWithLua(l, applyErr, "set deployment root").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LFalse)
		l.Push(err)
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}
