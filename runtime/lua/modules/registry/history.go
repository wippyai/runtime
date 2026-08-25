// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"

	lua "github.com/wippyai/go-lua"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"go.uber.org/zap"
)

// History represents access to the registry version history
type History struct {
	reg  regapi.Registry
	hist regapi.History
	log  *zap.Logger
}

// historyVersions returns all available versions in the registry history
func historyVersions(l *lua.LState) int {
	history := checkHistory(l)
	if history == nil {
		return 0
	}

	versions, versErr := history.hist.Versions()
	if versErr != nil {
		err := lua.WrapErrorWithLua(l, versErr, "get versions").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	versionsTable := l.CreateTable(len(versions), 0)
	for i, ver := range versions {
		value.PushTypedUserData(l, ver, typeVersion)
		versionsTable.RawSetInt(i+1, l.Get(-1))
		l.Pop(1)
	}

	l.Push(versionsTable)
	l.Push(lua.LNil)
	return 2
}

// historyGetVersion retrieves a specific version by ID
func historyGetVersion(l *lua.LState) int {
	history := checkHistory(l)
	if history == nil {
		return 0
	}

	vID := l.CheckNumber(2)
	if vID < 0 {
		err := lua.NewLuaError(l, "invalid version ID").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	foundVersion, lookupErr := findHistoryVersion(history.hist, uint(vID))
	if lookupErr != nil {
		err := lua.WrapErrorWithLua(l, lookupErr, "get version").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	if foundVersion == nil {
		err := lua.NewLuaError(l, "version not found").
			WithKind(lua.NotFound).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	value.PushTypedUserData(l, foundVersion, typeVersion)
	l.Push(lua.LNil)
	return 2
}

func findHistoryVersion(history regapi.History, id uint) (regapi.Version, error) {
	if lookup, ok := history.(regapi.VersionLookup); ok {
		return lookup.GetVersion(id)
	}
	versions, err := history.Versions()
	if err != nil {
		return nil, err
	}
	for _, stored := range versions {
		if stored.ID() == id {
			return stored, nil
		}
	}
	return nil, nil
}

// historySnapshotAt returns a snapshot of the registry at a specific version
func historySnapshotAt(l *lua.LState) int {
	history := checkHistory(l)
	if history == nil {
		return 0
	}

	ud := l.CheckUserData(2)
	version, ok := ud.Value.(regapi.Version)
	if !ok {
		err := lua.NewLuaError(l, "expected version object").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	provenanced, ok := history.reg.(interface {
		ProvenancedStateAtVersion(ctx context.Context, v regapi.Version) (regapi.ProvenancedState, error)
	})
	if !ok {
		err := lua.NewLuaError(l, "registry does not serve historical snapshots").
			WithKind(lua.Unavailable).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}
	state, buildErr := provenanced.ProvenancedStateAtVersion(l.Context(), version)
	if buildErr != nil {
		err := lua.WrapErrorWithLua(l, buildErr, "build snapshot state").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	snap := &Snapshot{
		reg:     history.reg,
		version: version,
		entries: state.Entries,
		prov:    state.Provenance,
		log:     history.log,
	}

	value.PushTypedUserData(l, snap, typeSnapshot)
	l.Push(lua.LNil)
	return 2
}

// checkHistory checks if the first argument is a History userdata
func checkHistory(l *lua.LState) *History {
	ud := l.CheckUserData(1)
	if history, ok := ud.Value.(*History); ok {
		return history
	}
	l.ArgError(1, "history expected")
	return nil
}
