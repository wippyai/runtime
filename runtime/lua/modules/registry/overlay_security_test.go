// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	transcoder "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	secsystem "github.com/wippyai/runtime/system/security"
)

type overlayPolicy struct {
	allowed map[string]struct{}
}

func (p overlayPolicy) ID() regapi.ID {
	return regapi.NewID("test", "overlay_policy")
}

func (p overlayPolicy) Evaluate(_ secapi.Actor, action, resource string, _ attrs.Bag) secapi.Result {
	if _, ok := p.allowed[action+"\x00"+resource]; ok {
		return secapi.Allow
	}
	return secapi.Deny
}

func strictOverlayContext(t *testing.T, allowed ...string) (context.Context, func()) {
	t.Helper()
	ctx := ctxapi.NewRootContext()
	dtt := transcoder.GlobalTranscoder()
	json.Register(dtt)
	luapayload.Register(dtt)
	ctx = payload.WithTranscoder(ctx, dtt)
	ctx = secapi.SetStrictMode(ctx, true)
	ctx, frame := ctxapi.OpenFrameContext(ctx)
	permissions := make(map[string]struct{}, len(allowed))
	for _, permission := range allowed {
		permissions[permission] = struct{}{}
	}
	require.NoError(t, secapi.SetActor(ctx, secapi.Actor{ID: "controller"}))
	require.NoError(t, secapi.SetScope(ctx, secsystem.NewScope([]secapi.Policy{overlayPolicy{allowed: permissions}})))
	return ctx, func() { ctxapi.ReleaseFrameContext(frame) }
}

func overlayTestRegistry(owner string, entries regapi.State) *mockRegistry {
	return &mockRegistry{
		entries:        map[string]regapi.Entry{},
		overlayEntries: map[string]regapi.State{owner: entries},
		currentVersion: &mockVersion{id: 3, str: "v3"},
	}
}

func runOverlayLua(ctx context.Context, t *testing.T, reg *mockRegistry, source string) {
	t.Helper()
	ctx = regapi.WithRegistry(ctx, reg)
	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)
	lua.OpenErrors(l)
	setupModule(l)
	require.NoError(t, l.DoString(source))
}

func TestOverlaySecurityRequiresOwnerRead(t *testing.T) {
	const owner = "data-sources:one"
	ctx, release := strictOverlayContext(t)
	defer release()
	reg := overlayTestRegistry(owner, nil)
	runOverlayLua(ctx, t, reg, `
		local snap, err = registry.overlay("data-sources:one")
		assert(snap == nil and err ~= nil)
		assert(err:kind() == errors.PERMISSION_DENIED)
	`)
	assert.Empty(t, reg.appliedChanges)
}

func TestOverlaySecurityChecksKindAndRealEntryID(t *testing.T) {
	const owner = "data-sources:one"
	entry := regapi.Entry{
		ID:   regapi.NewID("data-sources.runtime", "one"),
		Kind: "db.sql.postgres",
		Data: payload.NewPayload(map[string]any{"host": "db.internal"}, payload.Golang),
	}
	ctx, release := strictOverlayContext(t,
		"registry.overlay.get\x00"+owner,
		"registry.overlay.apply\x00"+owner,
		"registry.overlay.update.db.sql.postgres\x00data-sources.runtime:other",
	)
	defer release()
	reg := overlayTestRegistry(owner, regapi.State{entry})
	runOverlayLua(ctx, t, reg, `
		local snap = assert(registry.overlay("data-sources:one"))
		local entries = assert(snap:entries())
		entries[1].data.host = "changed.internal"
		local changes = snap:changes()
		changes:update(entries[1])
		local version, err = changes:apply()
		assert(version == nil and err ~= nil)
		assert(err:kind() == errors.PERMISSION_DENIED)
	`)
	assert.Empty(t, reg.appliedChanges)
}

func TestOverlaySecurityBulkDeleteIsAuthorizedAndAtomic(t *testing.T) {
	const owner = "data-sources:one"
	db := regapi.Entry{ID: regapi.NewID("data-sources.runtime", "one"), Kind: "db.sql.postgres"}
	env := regapi.Entry{ID: regapi.NewID("data-sources.env", "one_password"), Kind: "env.variable"}

	t.Run("authorized", func(t *testing.T) {
		ctx, release := strictOverlayContext(t,
			"registry.overlay.get\x00"+owner,
			"registry.overlay.apply\x00"+owner,
			"registry.overlay.delete.db.sql.postgres\x00"+db.ID.String(),
			"registry.overlay.delete.env.variable\x00"+env.ID.String(),
		)
		defer release()
		reg := overlayTestRegistry(owner, regapi.State{db, env})
		runOverlayLua(ctx, t, reg, `
			local snap = assert(registry.overlay("data-sources:one"))
			local entries = assert(snap:entries())
			local changes = snap:changes()
			changes:delete(entries)
			local _, err = changes:apply()
			assert(err == nil)
		`)
		require.Len(t, reg.appliedChanges, 2)
		assert.Equal(t, regapi.EntryDelete, reg.appliedChanges[0].Kind)
		assert.Equal(t, regapi.EntryDelete, reg.appliedChanges[1].Kind)
	})

	t.Run("one denied rejects entire changeset", func(t *testing.T) {
		ctx, release := strictOverlayContext(t,
			"registry.overlay.get\x00"+owner,
			"registry.overlay.apply\x00"+owner,
			"registry.overlay.delete.db.sql.postgres\x00"+db.ID.String(),
		)
		defer release()
		reg := overlayTestRegistry(owner, regapi.State{db, env})
		runOverlayLua(ctx, t, reg, `
			local snap = assert(registry.overlay("data-sources:one"))
			local entries = assert(snap:entries())
			local changes = snap:changes()
			changes:delete(entries)
			local version, err = changes:apply()
			assert(version == nil and err ~= nil)
			assert(err:kind() == errors.PERMISSION_DENIED)
		`)
		assert.Empty(t, reg.appliedChanges)
	})
}

func TestOverlayChangesOpsReauthorizesRetainedSnapshot(t *testing.T) {
	const owner = "data-sources:one"
	ctx, release := strictOverlayContext(t, "registry.overlay.get\x00"+owner)
	defer release()
	reg := overlayTestRegistry(owner, nil)
	ctx = regapi.WithRegistry(ctx, reg)
	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)
	lua.OpenErrors(l)
	setupModule(l)
	require.NoError(t, l.DoString(`
		snap = assert(registry.overlay("data-sources:one"))
		changes = snap:changes()
		changes:create({ id = "data-sources.runtime:one", kind = "db.sql.postgres" })
	`))
	require.NoError(t, secapi.SetScope(ctx, secsystem.NewScope([]secapi.Policy{overlayPolicy{allowed: map[string]struct{}{}}})))
	require.NoError(t, l.DoString(`
		local ops, err = changes:ops()
		assert(ops == nil and err ~= nil)
		assert(err:kind() == errors.PERMISSION_DENIED)
	`))
}
