// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
)

// Keep the existing Registry and History contracts in these adapter tests.
type resolutionTestRegistry struct {
	*mockRegistry
	hist       regapi.History
	resolution *regapi.DependencyResolution
}

func (r *resolutionTestRegistry) History() regapi.History { return r.hist }
func (r *resolutionTestRegistry) Plan(ctx context.Context, base regapi.Version, changes regapi.ChangeSet) (*regapi.Plan, error) {
	plan, err := r.mockRegistry.Plan(ctx, base, changes)
	if plan != nil {
		plan.Resolution = r.resolution
	}
	return plan, err
}

func testResolution(selected string) *regapi.DependencyResolution {
	return (&regapi.DependencyResolution{
		InputDigest: "sha256:inputs", BaselineDigest: "sha256:baseline",
		Deployment: &regapi.Deployment{Root: "org/app", Modules: []regapi.ResolvedModule{
			{Name: "org/app", Version: "1.0.0", Digest: "sha256:pinned"},
		}},
		Modules: []regapi.ResolvedModule{{Name: "org/app", Version: selected, Digest: "sha256:" + selected}},
	}).Canonical()
}

func TestResolutionReadPermissionAcrossLuaExports(t *testing.T) {
	storage := historymem.New()
	v1 := version.FromParent(version.New(0), 1)
	recorded, live := testResolution("1.0.0"), testResolution("2.0.0")
	entry := regapi.Entry{ID: regapi.NewID("app", "visible"), Kind: "registry.entry"}
	require.NoError(t, storage.SaveWithDependencyResolution(v1, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: entry}}, recorded, true))
	reg := &resolutionTestRegistry{mockRegistry: planTestMock(), hist: storage, resolution: live}
	reg.snapshot = regapi.Snapshot{Version: v1, Entries: regapi.State{entry}, Registry: regapi.StateMetadata{Resolution: live}}
	for _, allow := range []bool{false, true} {
		name := "denied"
		if allow {
			name = "allowed"
		}
		t.Run(name, func(t *testing.T) {
			grants := []string{"registry.get\x00app:visible", "registry.apply\x00app:visible", "registry.overlay.get\x00controller"}
			if allow {
				grants = append(grants, "registry.resolution.get\x00")
			}
			ctx, release := strictOverlayContext(t, grants...)
			defer release()
			source := `
    local snap = assert(registry.snapshot())
    local changes = assert(snap:changes())
    assert(changes:update({id="app:visible", kind="registry.entry"}))
    local plan, plan_err = changes:plan()
    assert(plan_err == nil and plan.base:id() == 1)
    assert(plan.changes[1].entry.id == "app:visible")
    local hist = assert(registry.history())
    local historical = assert(registry.snapshot_at(1))
    local via_history = assert(hist:snapshot_at(assert(hist:get_version(1))))
    for _, snapshot in ipairs({snap, historical, via_history}) do
     local state, err = snapshot:state()
     assert(err == nil and #state.entries == 1)
     assert(state.entries[1].id == "app:visible")
     if ALLOW then
      assert(state.resolution.lock.root_module == "org/app")
      assert(state.resolution.deployment == nil)
      assert(state.resolution.lock.modules[1].version == "1.0.0")
      assert(state.resolution.modules[1].version == (snapshot == snap and "2.0.0" or "1.0.0"))
     else
      assert(state.resolution == nil)
     end
    end
    if ALLOW then
     assert(plan.resolution.modules[1].version == "2.0.0")
     assert(plan.resolution.lock.root_module == "org/app")
     assert(plan.resolution.lock.modules[1].version == "1.0.0")
     local digest = plan.resolution.lock.digest
     assert(digest == historical:state().resolution.lock.digest)
     plan.resolution.lock.modules[1].version = "forged"
     assert(changes:plan().resolution.lock.modules[1].version == "1.0.0")
    else
     assert(plan.resolution == nil)
    end
   `
			if allow {
				source = "local ALLOW = true\n" + source
			} else {
				source = "local ALLOW = false\n" + source
			}
			runRegistryLua(ctx, t, reg, source)
		})
	}
}

func TestCapturedResolutionRechecksReadPermission(t *testing.T) {
	allowed, releaseAllowed := strictOverlayContext(t, "registry.resolution.get\x00", "registry.apply\x00app:new")
	defer releaseAllowed()
	denied, releaseDenied := strictOverlayContext(t, "registry.apply\x00app:new")
	defer releaseDenied()
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	setupModule(l)
	resolution := testResolution("2.0.0")
	reg := &resolutionTestRegistry{mockRegistry: planTestMock(), resolution: resolution}
	reg.snapshot = regapi.Snapshot{Version: reg.currentVersion, Registry: regapi.StateMetadata{Resolution: resolution}}
	l.SetContext(regapi.WithRegistry(allowed, reg))
	require.NoError(t, l.DoString(`
  snap = assert(registry.snapshot())
  changes = assert(snap:changes())
  assert(changes:create({id="app:new", kind="registry.entry"}))
  assert(snap:state().resolution.lock ~= nil)
  assert(changes:plan().resolution.lock ~= nil)
 `))
	l.SetContext(regapi.WithRegistry(denied, reg))
	require.NoError(t, l.DoString(`
  local state, state_err = snap:state()
  local plan, plan_err = changes:plan()
  assert(state_err == nil and plan_err == nil)
  assert(state.resolution == nil and plan.resolution == nil)
 `))
	l.SetContext(regapi.WithRegistry(allowed, reg))
	require.NoError(t, l.DoString(`assert(snap:state().resolution.lock ~= nil); assert(changes:plan().resolution.lock ~= nil)`))
}

func TestLockDigestPinsRootAndExactModules(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	first := testResolution("1.0.0")
	lock := func(r *regapi.DependencyResolution) *lua.LTable {
		return resolutionToLuaTable(l, r).RawGetString("lock").(*lua.LTable)
	}
	digest := lock(first).RawGetString("digest")
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, digest.String())
	require.Equal(t, digest, lock(testResolution("2.0.0")).RawGetString("digest"))
	for _, mutate := range []func(*regapi.Deployment){
		func(d *regapi.Deployment) { d.Root = "other/app" },
		func(d *regapi.Deployment) { d.Modules[0].Version = "3.0.0" },
		func(d *regapi.Deployment) { d.Modules[0].Digest = "sha256:different" },
	} {
		changed := first.Canonical()
		mutate(changed.Deployment)
		require.NotEqual(t, digest, lock(changed).RawGetString("digest"))
	}
	first.Deployment.Modules = append(first.Deployment.Modules, regapi.ResolvedModule{Name: "aaa/library", Version: "1.0.0", Digest: "sha256:library"})
	ordered := first.Canonical()
	require.Equal(t, lock(first).RawGetString("digest"), lock(ordered).RawGetString("digest"))
	noLock := first.Canonical()
	noLock.Deployment = nil
	require.Equal(t, lua.LNil, resolutionToLuaTable(l, noLock).RawGetString("lock"))
}

type resolutionReadFailure struct{ regapi.ResolutionHistory }

func (resolutionReadFailure) GetDependencyResolution(regapi.Version) (*regapi.DependencyResolution, error) {
	return nil, errors.New("storage unavailable")
}

func TestHistoricalResolutionAbsenceAndStorageFailure(t *testing.T) {
	storage := historymem.New()
	v1 := version.FromParent(version.New(0), 1)
	require.NoError(t, storage.Save(v1, nil, true))
	// Wrapping only the original History contract simulates a legacy provider.
	for _, hist := range []regapi.History{storage, struct{ regapi.History }{storage}} {
		reg := &resolutionTestRegistry{mockRegistry: planTestMock(), hist: hist}
		runRegistryLua(setupContextWithTranscoder(), t, reg, `
   local hist = assert(registry.history())
   local a = assert(registry.snapshot_at(1)):state()
   local b = assert(hist:snapshot_at(assert(hist:get_version(1)))):state()
   assert(a.resolution == nil and b.resolution == nil)
  `)
	}
	reg := &resolutionTestRegistry{mockRegistry: planTestMock(), hist: resolutionReadFailure{storage}}
	runRegistryLua(setupContextWithTranscoder(), t, reg, `
  local hist = assert(registry.history())
  local a, ae = registry.snapshot_at(1)
  local b, be = hist:snapshot_at(assert(hist:get_version(1)))
  assert(a ~= nil and b ~= nil and ae == nil and be == nil)
  assert(a:state().resolution == nil and b:state().resolution == nil)
 `)
}
