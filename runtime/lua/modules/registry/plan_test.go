// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
)

// unfencedRegistry exposes only the base Registry surface, so it cannot
// fence an apply on a version. The Lua module must refuse it rather than fall
// back to an unfenced apply.
type unfencedRegistry struct {
	regapi.Registry
}

func runRegistryLua(ctx context.Context, t *testing.T, reg regapi.Registry, source string) {
	t.Helper()
	ctx = regapi.WithRegistry(ctx, reg)
	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)
	lua.OpenErrors(l)
	setupModule(l)
	require.NoError(t, l.DoString(source))
}

func planTestMock() *mockRegistry {
	return &mockRegistry{
		entries:        map[string]regapi.Entry{},
		overlayEntries: map[string]regapi.State{},
		currentVersion: &mockVersion{id: 7, str: "v7"},
	}
}

func TestChangesApplyIsFencedOnTheSnapshotVersion(t *testing.T) {
	reg := planTestMock()
	runRegistryLua(setupContextWithTranscoder(), t, reg, `
		local snap = assert(registry.snapshot())
		local changes = snap:changes()
		changes:create({ id = "app:svc", kind = "registry.entry", data = { name = "svc" } })
		local version, err = changes:apply()
		assert(err == nil, tostring(err))
		assert(version ~= nil)
	`)
	require.NotNil(t, reg.appliedBase, "apply must carry the version the snapshot was read at")
	assert.Equal(t, uint(7), reg.appliedBase.ID())
	assert.Nil(t, reg.appliedPlan, "no plan was computed, so none is bound")
	require.Len(t, reg.appliedChanges, 1)
}

func TestChangesPlanBindsTheApply(t *testing.T) {
	reg := planTestMock()
	runRegistryLua(setupContextWithTranscoder(), t, reg, `
		local snap = assert(registry.snapshot())
		local changes = snap:changes()
		changes:create({ id = "app:svc", kind = "registry.entry", data = { name = "svc" } })

		local plan, err = changes:plan()
		assert(err == nil, tostring(err))
		assert(plan.digest == "plan-digest-v7", plan.digest)
		assert(plan.base:id() == 7)
		assert(#plan.changes == 1)
		assert(plan.changes[1].op == "create")
		assert(plan.changes[1].entry.id == "app:svc")
		assert(plan.changes[1].entry.kind == "registry.entry")
		assert(#plan.history == 1)
		assert(#plan.effects == 1)
		assert(plan.effects[1].kind == "test.effect")
		assert(plan.effects[1].digest == "effect-digest")

		local version, apply_err = changes:apply()
		assert(apply_err == nil, tostring(apply_err))
		assert(version ~= nil)
	`)
	require.NotNil(t, reg.plannedBase)
	assert.Equal(t, uint(7), reg.plannedBase.ID())
	require.NotNil(t, reg.appliedPlan, "apply after a successful plan is bound to it")
	assert.Equal(t, "plan-digest-v7", reg.appliedPlan.Digest)
}

func TestMutatingChangesAfterPlanDropsTheBinding(t *testing.T) {
	reg := planTestMock()
	runRegistryLua(setupContextWithTranscoder(), t, reg, `
		local snap = assert(registry.snapshot())
		local changes = snap:changes()
		changes:create({ id = "app:svc", kind = "registry.entry", data = { name = "svc" } })
		assert(changes:plan())
		changes:create({ id = "app:other", kind = "registry.entry", data = { name = "other" } })
		local _, err = changes:apply()
		assert(err == nil, tostring(err))
	`)
	assert.Nil(t, reg.appliedPlan, "a changed changeset is no longer the reviewed plan")
	require.NotNil(t, reg.appliedBase, "the apply is still fenced on the snapshot version")
	require.Len(t, reg.appliedChanges, 2)
}

func TestChangesPlanRequiresOperations(t *testing.T) {
	reg := planTestMock()
	runRegistryLua(setupContextWithTranscoder(), t, reg, `
		local snap = assert(registry.snapshot())
		local plan, err = snap:changes():plan()
		assert(plan == nil and err ~= nil)
		assert(err:kind() == errors.INVALID)
	`)
	assert.Nil(t, reg.plannedBase)
}

func TestChangesApplyRefusesARegistryThatCannotFence(t *testing.T) {
	mock := planTestMock()
	runRegistryLua(setupContextWithTranscoder(), t, unfencedRegistry{Registry: mock}, `
		local snap = assert(registry.snapshot())
		local changes = snap:changes()
		changes:create({ id = "app:svc", kind = "registry.entry", data = { name = "svc" } })
		local version, err = changes:apply()
		assert(version == nil and err ~= nil)
		assert(err:kind() == errors.INTERNAL)
		assert(err:retryable() == false)
	`)
	assert.Nil(t, mock.appliedBase, "nothing may be applied without a fence")
	assert.Empty(t, mock.appliedChanges)
}

func TestDurableApplyAuthorizesEachOperation(t *testing.T) {
	stored := regapi.Entry{
		ID:   regapi.NewID("app", "old"),
		Kind: "db.sql.postgres",
		Data: payload.NewPayload(map[string]any{"host": "db.internal"}, payload.Golang),
	}
	source := `
		local snap = assert(registry.snapshot())
		local changes = snap:changes()
		changes:create({ id = "app:svc", kind = "registry.entry", data = { name = "svc" } })
		changes:delete("app:old")
		return changes:apply()
	`
	t.Run("one operation denied refuses the whole changeset", func(t *testing.T) {
		ctx, release := strictOverlayContext(t, "registry.apply\x00app:svc")
		defer release()
		reg := planTestMock()
		reg.snapshot = regapi.Snapshot{Version: reg.currentVersion, Entries: regapi.State{stored}}
		runRegistryLua(ctx, t, reg, `
			local version, err = (function() `+source+` end)()
			assert(version == nil and err ~= nil)
			assert(err:kind() == errors.PERMISSION_DENIED)
			assert(err:details().entry_id == "app:old")
		`)
		assert.Nil(t, reg.appliedBase)
		assert.Empty(t, reg.appliedChanges)
	})
	t.Run("every operation granted applies", func(t *testing.T) {
		ctx, release := strictOverlayContext(t,
			"registry.apply\x00app:svc",
			"registry.apply\x00app:old",
		)
		defer release()
		reg := planTestMock()
		reg.snapshot = regapi.Snapshot{Version: reg.currentVersion, Entries: regapi.State{stored}}
		runRegistryLua(ctx, t, reg, `
			local version, err = (function() `+source+` end)()
			assert(err == nil, tostring(err))
		`)
		require.Len(t, reg.appliedChanges, 2)
	})
	t.Run("a grant on an unrelated entry covers nothing", func(t *testing.T) {
		ctx, release := strictOverlayContext(t, "registry.apply\x00app:unrelated")
		defer release()
		reg := planTestMock()
		reg.snapshot = regapi.Snapshot{Version: reg.currentVersion, Entries: regapi.State{stored}}
		runRegistryLua(ctx, t, reg, `
			local version, err = (function() `+source+` end)()
			assert(version == nil and err ~= nil)
			assert(err:kind() == errors.PERMISSION_DENIED)
		`)
		assert.Empty(t, reg.appliedChanges)
	})
	t.Run("plan is held to the same authorization as apply", func(t *testing.T) {
		ctx, release := strictOverlayContext(t, "registry.apply\x00app:unrelated")
		defer release()
		reg := planTestMock()
		runRegistryLua(ctx, t, reg, `
			local snap = assert(registry.snapshot())
			local changes = snap:changes()
			changes:create({ id = "app:svc", kind = "registry.entry", data = { name = "svc" } })
			local plan, err = changes:plan()
			assert(plan == nil and err ~= nil)
			assert(err:kind() == errors.PERMISSION_DENIED)
		`)
		assert.Nil(t, reg.plannedBase, "a denied plan computes nothing")
	})
}
