// SPDX-License-Identifier: MPL-2.0

package registry

import (
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

// makeBuildDelta creates a build_delta function with the given logger
func makeBuildDelta(log *zap.Logger) lua.LGoFunc {
	return func(l *lua.LState) int {
		return buildDelta(l, log)
	}
}

// buildDelta creates a changeset for transitioning from one state to another
func buildDelta(l *lua.LState, log *zap.Logger) int {
	fromTable := l.CheckTable(1)
	if fromTable == nil {
		err := lua.NewLuaError(l, "from_entries table required").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	toTable := l.CheckTable(2)
	if toTable == nil {
		err := lua.NewLuaError(l, "to_entries table required").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	fromEntries := make(regapi.State, 0)
	toEntries := make(regapi.State, 0)

	fromTable.ForEach(func(_, v lua.LValue) {
		if entryTable, ok := v.(*lua.LTable); ok {
			entry, err := luaTableToEntry(l, entryTable)
			if err == nil {
				fromEntries = append(fromEntries, entry)
			} else if log != nil {
				log.Debug("error converting entry", zap.Error(err))
			}
		}
	})

	toTable.ForEach(func(_, v lua.LValue) {
		if entryTable, ok := v.(*lua.LTable); ok {
			entry, err := luaTableToEntry(l, entryTable)
			if err == nil {
				toEntries = append(toEntries, entry)
			} else if log != nil {
				log.Debug("error converting entry", zap.Error(err))
			}
		}
	})

	dtt := payload.GetTranscoder(l.Context())
	if dtt == nil {
		err := lua.NewLuaError(l, "transcoder not available").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	resolver := regapi.GetResolver(l.Context())
	stateBuilder := topology.NewStateBuilder(log, resolver, topology.WithCompareFunc(func(a, b regapi.Entry) bool {
		if a.ID.NS != b.ID.NS || a.ID.Name != b.ID.Name || a.Kind != b.Kind {
			return false
		}
		if !mapsEqual(map[string]any(a.Meta), map[string]any(b.Meta)) {
			return false
		}

		aMap := make(map[string]any)
		bMap := make(map[string]any)

		if err := dtt.Unmarshal(a.Data, &aMap); err != nil {
			return false
		}
		if err := dtt.Unmarshal(b.Data, &bMap); err != nil {
			return false
		}

		return mapsEqual(aMap, bMap)
	}))

	changeSet, buildErr := stateBuilder.BuildDelta(fromEntries, toEntries)
	if buildErr != nil {
		err := lua.WrapErrorWithLua(l, buildErr, "build delta").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	resultTable := l.CreateTable(len(changeSet), 0)
	for i, op := range changeSet {
		opTable := l.CreateTable(0, 2)
		opTable.RawSetString("kind", lua.LString(op.Kind))

		entryTable, convErr := entryToLuaTable(l, op.Entry)
		if convErr != nil {
			err := lua.WrapErrorWithLua(l, convErr, "convert entry").
				WithKind(lua.Internal).
				WithRetryable(false)
			l.Push(lua.LNil)
			l.Push(err)
			return 2
		}

		opTable.RawSetString("entry", entryTable)
		resultTable.RawSetInt(i+1, opTable)
	}

	l.Push(resultTable)
	l.Push(lua.LNil)
	return 2
}
