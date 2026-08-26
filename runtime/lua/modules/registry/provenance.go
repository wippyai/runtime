// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"

	lua "github.com/wippyai/go-lua"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/security"
)

// provenanceReader answers provenance questions about the live registry.
type provenanceReader interface {
	EntryProvenance(regapi.ID) (regapi.EntryProvenance, bool)
}

func provenanceToLuaTable(l *lua.LState, p regapi.EntryProvenance) *lua.LTable {
	table := l.CreateTable(0, 4)
	table.RawSetString("module", lua.LString(p.Module))
	table.RawSetString("version", lua.LString(p.Version))
	table.RawSetString("digest", lua.LString(p.Digest))
	table.RawSetString("root", lua.LBool(p.Root))
	return table
}

// registryProvenance returns the provenance record for one entry of the live
// state: {module, version, digest, root}, or nil for an unknown entry. Values
// are copies; mutating the table changes nothing.
func registryProvenance(l *lua.LState) int {
	id := regapi.ParseID(l.CheckString(1)).Canonical()
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no context").
			WithKind(lua.Internal).
			WithRetryable(false))
		return 2
	}

	reg := regapi.GetRegistry(ctx)
	if reg == nil {
		err := lua.NewLuaError(l, "registry not available").
			WithKind(lua.Unavailable).
			WithRetryable(true)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}
	if !security.IsAllowed(ctx, "registry.get", id.String(), nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "not allowed to access entry: "+id.String()).
			WithKind(lua.PermissionDenied).
			WithRetryable(false))
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

	p, found := reader.EntryProvenance(id)
	if !found {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	l.Push(provenanceToLuaTable(l, p))
	l.Push(lua.LNil)
	return 2
}

// rootSetter is the registry-side atomic root mutation.
type rootSetter interface {
	SetDependencyRoot(ctx context.Context, id regapi.ID, root bool) (regapi.Version, error)
}

// registrySetRoot flips the deployment-root selection of one ns.dependency
// entry. Root-ness is deployment state held by the registry, not entry
// content: the mutation is authorized like any governance apply and runs
// inside the registry's apply serialization, changing only the Root flag on
// the current tuple.
func registrySetRoot(l *lua.LState) int {
	idStr := l.CheckString(1)
	enable := l.CheckBool(2)

	if !security.IsAllowed(l.Context(), "registry.apply", "", nil) {
		err := lua.NewLuaError(l, "not allowed to apply registry changes").
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LFalse)
		l.Push(err)
		return 2
	}

	reg := regapi.GetRegistry(l.Context())
	if reg == nil {
		err := lua.NewLuaError(l, "registry not available").
			WithKind(lua.Unavailable).
			WithRetryable(true)
		l.Push(lua.LFalse)
		l.Push(err)
		return 2
	}
	setter, ok := reg.(rootSetter)
	if !ok {
		err := lua.NewLuaError(l, "registry does not serve deployment-root mutation").
			WithKind(lua.Unavailable).
			WithRetryable(false)
		l.Push(lua.LFalse)
		l.Push(err)
		return 2
	}

	if _, setErr := setter.SetDependencyRoot(l.Context(), regapi.ParseID(idStr), enable); setErr != nil {
		err := lua.WrapErrorWithLua(l, setErr, "set deployment root").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LFalse)
		l.Push(err)
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}
