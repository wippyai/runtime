// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/logs"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/system/registry/finder"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type filterWarningRegistry struct {
	*mockRegistry
	all []regapi.Entry
}

func (r *filterWarningRegistry) GetAllEntries() ([]regapi.Entry, error) { return r.all, nil }

func TestLegacyFindFiltersWarnWithoutChangingResults(t *testing.T) {
	for _, snapshot := range []bool{false, true} {
		t.Run(map[bool]string{false: "registry", true: "snapshot"}[snapshot], func(t *testing.T) {
			ctx := setupContextWithTranscoder()
			core, output := observer.New(zap.WarnLevel)
			ctx = logs.WithLogger(ctx, zap.New(core))
			entries := []regapi.Entry{
				{ID: regapi.NewID("app", "a"), Kind: "process.lua", Meta: attrs.Bag{"type": "tool"}},
				{ID: regapi.NewID("app", "b"), Kind: "function.lua", Meta: attrs.Bag{"type": "test"}},
			}
			reg := &filterWarningRegistry{mockRegistry: &mockRegistry{entries: map[string]regapi.Entry{"app:a": entries[0], "app:b": entries[1]}}, all: entries}
			ctx = regapi.WithRegistry(ctx, reg)
			ctx = regapi.WithFinder(ctx, finder.NewFinder(reg, zap.NewNop()))
			l := lua.NewState()
			defer l.Close()
			l.SetContext(ctx)
			setupModule(l)
			call := "registry.find"
			if snapshot {
				value.PushTypedUserData(l, &Snapshot{reg: reg, entries: entries, log: zap.NewNop()}, typeSnapshot)
				l.SetGlobal("snap", l.Get(-1))
				l.Pop(1)
				call = "snap:find"
			}
			require.NoError(t, l.DoString(`
				local function find(filter, n)
					local entries, err = `+call+`(filter)
					assert(err == nil and #entries == n)
				end
				find({}, 2)
				find({[".kind"]="process.lua"}, 1)
				find({["meta.type"]="tool"}, 1)
				find({["~meta.type"]="^to"}, 1)
			`))
			require.Zero(t, output.Len())
			require.NoError(t, l.DoString(`
				local function find(filter, n)
					local entries, err = `+call+`(filter)
					assert(err == nil and #entries == n)
				end
				find({kind="process.lua", type="tool"}, 2)
				find({meta={type="tool"}}, 2)
				find({[1]="ignored", meta=false}, 2)
				find({["meta.type"]="tool", meta={["meta.type"]="test"}}, 1)
			`))
			require.Equal(t, 4, output.Len(), "one warning per legacy call, not per key")
			for _, entry := range output.All() {
				require.Contains(t, entry.Message, "deprecated")
				require.Contains(t, entry.Message, "legacy behavior is preserved")
			}
		})
	}
}
