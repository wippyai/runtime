// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	"github.com/wippyai/runtime/internal/version"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	embedpkg "github.com/wippyai/runtime/service/fs/embed"
	"github.com/wippyai/runtime/system/eventbus"
	systemfs "github.com/wippyai/runtime/system/fs"
	sysregistry "github.com/wippyai/runtime/system/registry"
	"github.com/wippyai/runtime/system/registry/events"
	"github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/runner"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

const (
	transitionReleaseKind regapi.Kind = "test.release"
	transitionWasmKind    regapi.Kind = "function.wasm"
	transitionGateKind    regapi.Kind = "test.gate"
	transitionModule                  = "org/mod"
	transitionWasmPath                = "component.wasm"
)

var (
	transitionFSID      = regapi.NewID("app", "assets")
	transitionFuncID    = regapi.NewID("app", "func")
	transitionGateID    = regapi.NewID("app", "gate")
	transitionReleaseID = regapi.NewID("app", "release")
)

// wasmVerifier stands in for the function.wasm manager: every Add and Update
// loads the component through the filesystem registry and verifies its hash
// with the runtime's LoadAndVerifyWASM, exactly as the wasm function manager
// does before it swaps a function.
type wasmVerifier struct {
	fsReg    fsapi.Registry
	current  map[regapi.ID]string
	during   func()
	failures []error
	mu       sync.Mutex
}

func (v *wasmVerifier) Add(_ context.Context, entry regapi.Entry) error {
	return v.verify(entry)
}

func (v *wasmVerifier) Update(_ context.Context, entry regapi.Entry) error {
	if v.during != nil {
		v.during()
	}
	return v.verify(entry)
}

func (v *wasmVerifier) Delete(_ context.Context, entry regapi.Entry) error {
	v.mu.Lock()
	delete(v.current, entry.ID)
	v.mu.Unlock()
	return nil
}

func (v *wasmVerifier) verify(entry regapi.Entry) error {
	data, _ := entry.Data.Data().(map[string]any)
	fsID, _ := data["fs"].(string)
	hash, _ := data["hash"].(string)
	if _, err := wasmcomponent.LoadAndVerifyWASM(v.fsReg, fsID, transitionWasmPath, hash); err != nil {
		v.mu.Lock()
		v.failures = append(v.failures, err)
		v.mu.Unlock()
		return err
	}
	v.mu.Lock()
	v.current[entry.ID] = hash
	v.mu.Unlock()
	return nil
}

func (v *wasmVerifier) hash(id regapi.ID) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.current[id]
}

func (v *wasmVerifier) verificationFailures() []error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]error(nil), v.failures...)
}

// gateListener accepts its entry unless the entry asks to fail, standing in
// for any later operation of a module update that a listener rejects.
type gateListener struct{}

func (gateListener) Add(context.Context, regapi.Entry) error { return nil }

func (gateListener) Update(_ context.Context, entry regapi.Entry) error {
	data, _ := entry.Data.Data().(map[string]any)
	if fail, _ := data["fail"].(bool); fail {
		return errors.New("gate rejects the update")
	}
	return nil
}

func (gateListener) Delete(context.Context, regapi.Entry) error { return nil }

// releaseDirective expands a release entry into the operations and embedded
// pack effect a hub module update produces.
type releaseDirective struct {
	expand func(version string) regapi.DirectiveResult
}

func (d releaseDirective) Expand(_ context.Context, op regapi.Operation, _ regapi.State) (regapi.DirectiveResult, error) {
	data, _ := op.Entry.Data.Data().(map[string]any)
	release, _ := data["version"].(string)
	return d.expand(release), nil
}

// transitionHarness wires the path a hub module update drives through the
// runtime: the registry with its directive and effects, the bus runner, the
// embed Manager and a function.wasm verifier as registry listeners, the
// filesystem registry, and the embed pack registry.
type transitionHarness struct {
	ctx      context.Context
	reg      *sysregistry.Reg
	fsReg    *systemfs.Registry
	embedReg *embedpkg.Registry
	verifier *wasmVerifier
	dir      string
}

func newTransitionHarness(t *testing.T, directive releaseDirective) *transitionHarness {
	t.Helper()
	dir := t.TempDir()
	log := zap.NewNop()
	ctx, cancel := context.WithCancel(newTestContext())
	t.Cleanup(cancel)

	bus := eventbus.NewBus()
	t.Cleanup(bus.Stop)
	ctx = event.WithBus(ctx, bus)
	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	t.Cleanup(func() { _ = awaitSvc.Stop() })
	ctx = event.WithAwaitService(ctx, awaitSvc)

	fsReg := systemfs.NewRegistry(bus, log)
	require.NoError(t, fsReg.Start(ctx))
	t.Cleanup(func() { _ = fsReg.Stop() })

	embedReg := embedpkg.NewRegistry()
	t.Cleanup(func() { _ = embedReg.Close() })
	verifier := &wasmVerifier{fsReg: fsReg, current: make(map[regapi.ID]string)}

	router, err := eventbus.StartRouter(ctx, bus)
	require.NoError(t, err)
	t.Cleanup(func() { _ = router.Stop() })
	manager := embedpkg.NewManager(bus, payload.GetTranscoder(ctx), embedReg, log)
	require.NoError(t, router.AddHandler(events.NewRegistryHandler(embedapi.Kind, manager)))
	require.NoError(t, router.AddHandler(events.NewRegistryHandler(transitionWasmKind, verifier)))
	require.NoError(t, router.AddHandler(events.NewRegistryHandler(transitionGateKind, gateListener{})))

	resolver := topology.NewResolver()
	require.NoError(t, resolver.RegisterPattern(regapi.DependencyPattern{Path: "data.fs"}))
	require.NoError(t, resolver.RegisterPattern(regapi.DependencyPattern{Path: "data.after"}))
	builder := topology.NewStateBuilder(log, resolver)
	busRunner := runner.NewBusRunner(bus, log, builder,
		runner.WithDispatchPolicy(runner.NewKindDispatchPolicy([]regapi.Kind{transitionReleaseKind})))
	reg := sysregistry.NewRegistry(memory.New(), busRunner, builder, resolver, log,
		sysregistry.WithKindDirective(transitionReleaseKind, directive))
	require.NoError(t, reg.LoadState(ctx, nil, version.New(0)))

	return &transitionHarness{
		ctx:      ctx,
		reg:      reg,
		fsReg:    fsReg,
		embedReg: embedReg,
		verifier: verifier,
		dir:      dir,
	}
}

func (h *transitionHarness) packPath(release string) string {
	return filepath.Join(h.dir, "org", "mod-"+release+".wapp")
}

// writeRelease packs the module's embedded filesystem for release.
func (h *transitionHarness) writeRelease(t *testing.T, release string) {
	t.Helper()
	writeResourceWapp(t, h.packPath(release), transitionFSID.NS, transitionFSID.Name,
		map[string]string{transitionWasmPath: wasmBytes(release)})
}

// install boots release 1 the way a cold start does: the pack is registered
// active and its entries are created through the registry.
func (h *transitionHarness) install(t *testing.T) {
	t.Helper()
	h.writeRelease(t, "1")
	reader, file := openWapp(t, h.packPath("1"))
	require.NoError(t, h.embedReg.RegisterPack(h.packPath("1"), transitionModule, "1", reader, file))
	_, err := h.reg.Apply(h.ctx, regapi.ChangeSet{
		{Kind: regapi.EntryCreate, Entry: fsEntry("1")},
		{Kind: regapi.EntryCreate, Entry: funcEntry("1")},
		{Kind: regapi.EntryCreate, Entry: gateEntry(false)},
		{Kind: regapi.EntryCreate, Entry: releaseEntry("1")},
	})
	require.NoError(t, err)
	require.Equal(t, wasmHash("1"), h.verifier.hash(transitionFuncID))
}

func (h *transitionHarness) upgrade() error {
	_, err := h.reg.Apply(h.ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: releaseEntry("2")}})
	return err
}

func (h *transitionHarness) servedWasm(t *testing.T) string {
	t.Helper()
	fsys, ok := h.fsReg.GetFS(transitionFSID.String())
	require.True(t, ok)
	return readFSFile(t, fsys)
}

func (h *transitionHarness) registryHash(t *testing.T) string {
	t.Helper()
	entry, err := h.reg.GetEntry(transitionFuncID)
	require.NoError(t, err)
	data, _ := entry.Data.Data().(map[string]any)
	hash, _ := data["hash"].(string)
	return hash
}

// upgradeDirective produces release 2 of the module: a changed embedded
// filesystem (its content digest changed), a function.wasm with the new hash,
// and optionally a later operation that fails.
func upgradeDirective(h **transitionHarness, failLater bool) releaseDirective {
	return releaseDirective{expand: func(release string) regapi.DirectiveResult {
		if release != "2" {
			return regapi.DirectiveResult{}
		}
		harness := *h
		return regapi.DirectiveResult{
			Applied: true,
			Additional: []regapi.ScopedOperation{
				{Operation: regapi.Operation{Kind: regapi.EntryUpdate, Entry: fsEntry("2")}},
				{Operation: regapi.Operation{Kind: regapi.EntryUpdate, Entry: funcEntry("2")}},
				{Operation: regapi.Operation{Kind: regapi.EntryUpdate, Entry: gateEntry(failLater)}},
			},
			Effects: []regapi.Effect{newEffect(harness.embedReg,
				[]stagedPack{{packPath: harness.packPath("2"), module: transitionModule, version: "2"}},
				[]obsoletePack{{module: transitionModule, version: "1"}})},
		}
	}}
}

func TestEmbedTransition_UpdatedFilesystemVerifiesNewWasmAgainstStagedPack(t *testing.T) {
	var h *transitionHarness
	h = newTransitionHarness(t, upgradeDirective(&h, false))
	h.install(t)
	h.writeRelease(t, "2")

	require.NoError(t, h.upgrade())

	assert.Empty(t, h.verifier.verificationFailures())
	assert.Equal(t, wasmHash("2"), h.verifier.hash(transitionFuncID))
	assert.Equal(t, wasmHash("2"), h.registryHash(t))
	assert.Equal(t, wasmBytes("2"), h.servedWasm(t))
	assert.True(t, h.embedReg.HasModulePack(transitionModule, "2"))
	assert.False(t, h.embedReg.HasModulePack(transitionModule, "1"))
}

func TestEmbedTransition_FailedUpgradeReverifiesPreviousWasm(t *testing.T) {
	var h *transitionHarness
	h = newTransitionHarness(t, upgradeDirective(&h, true))
	h.install(t)
	h.writeRelease(t, "2")

	err := h.upgrade()
	require.Error(t, err)

	// The forward function.wasm Update verified against the staged pack; the
	// only permitted verification failure is none: the reverse Update must
	// re-verify the previous hash against the previous pack.
	assert.Empty(t, h.verifier.verificationFailures())
	assert.Equal(t, wasmHash("1"), h.verifier.hash(transitionFuncID))
	assert.Equal(t, wasmHash("1"), h.registryHash(t), "registry state must match the listeners after rollback")
	assert.Equal(t, wasmBytes("1"), h.servedWasm(t))
	assert.True(t, h.embedReg.HasModulePack(transitionModule, "1"))
	assert.False(t, h.embedReg.HasModulePack(transitionModule, "2"))
}

func TestEmbedTransition_OutsideHandleReadsActivePackUntilCommit(t *testing.T) {
	for _, test := range []struct {
		name      string
		after     string
		failLater bool
	}{
		{name: "commit", failLater: false, after: "2"},
		{name: "rollback", failLater: true, after: "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var h *transitionHarness
			h = newTransitionHarness(t, upgradeDirective(&h, test.failLater))
			h.install(t)
			h.writeRelease(t, "2")

			outside, ok := h.fsReg.GetFS(transitionFSID.String())
			require.True(t, ok)
			var duringTransition []string
			h.verifier.during = func() {
				duringTransition = append(duringTransition, readFSFile(t, outside))
			}

			err := h.upgrade()
			if test.failLater {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.NotEmpty(t, duringTransition)
			for _, read := range duringTransition {
				assert.Equal(t, wasmBytes("1"), read, "a handle issued before the transition reads the active pack until commit")
			}
			assert.Equal(t, wasmBytes(test.after), readFSFile(t, outside))
		})
	}
}

func wasmBytes(release string) string {
	return "wasm component release " + release
}

func wasmHash(release string) string {
	sum := sha256.Sum256([]byte(wasmBytes(release)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestOf(release string) string {
	sum := sha256.Sum256([]byte("fs " + release))
	return hex.EncodeToString(sum[:])
}

func fsEntry(release string) regapi.Entry {
	return regapi.Entry{
		ID:   transitionFSID,
		Kind: embedapi.Kind,
		Data: payload.New(map[string]any{"digest": digestOf(release)}),
	}
}

func funcEntry(release string) regapi.Entry {
	return regapi.Entry{
		ID:   transitionFuncID,
		Kind: transitionWasmKind,
		Data: payload.New(map[string]any{"fs": transitionFSID.String(), "hash": wasmHash(release)}),
	}
}

func gateEntry(fail bool) regapi.Entry {
	return regapi.Entry{
		ID:   transitionGateID,
		Kind: transitionGateKind,
		Data: payload.New(map[string]any{"after": transitionFuncID.String(), "fail": fail}),
	}
}

func releaseEntry(release string) regapi.Entry {
	return regapi.Entry{
		ID:   transitionReleaseID,
		Kind: transitionReleaseKind,
		Data: payload.New(map[string]any{"version": release}),
	}
}

func readFSFile(t *testing.T, fsys fsapi.FS) string {
	t.Helper()
	file, err := fsys.Open(transitionWasmPath)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(file)
	require.NoError(t, err)
	return string(data)
}

func openWapp(t *testing.T, path string) (*wapp.Reader, *os.File) {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	reader, err := wapp.NewReader(file)
	if err != nil {
		_ = file.Close()
	}
	require.NoError(t, err)
	return reader, file
}
