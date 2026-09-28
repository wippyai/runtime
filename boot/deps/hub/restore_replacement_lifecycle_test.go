// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	moduleapi "github.com/wippyai/runtime/api/modules"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/lock"
	registryimpl "github.com/wippyai/runtime/system/registry"
	"github.com/wippyai/runtime/system/registry/expansion"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	historysqlite "github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

func supportsReplacementSymlinks(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("probe"), 0o600); err != nil {
		return false
	}
	return os.Symlink(target, filepath.Join(dir, "link")) == nil
}

func assertErrorIdentifiesModule(t *testing.T, err error, module string) {
	t.Helper()
	var apiErr apierror.Error
	if strings.Contains(err.Error(), module) ||
		(errors.As(err, &apiErr) && apiErr.Details() != nil && strings.Contains(apiErr.Details().GetString("module", ""), module)) {
		return
	}
	t.Fatalf("expected error to identify module %q, got: %v", module, err)
}

func assertErrorIdentifiesPath(t *testing.T, err error, path string) {
	t.Helper()
	cleanPath := filepath.ToSlash(filepath.Clean(path))
	var apiErr apierror.Error
	if strings.Contains(filepath.ToSlash(err.Error()), cleanPath) ||
		(errors.As(err, &apiErr) && apiErr.Details() != nil && strings.Contains(filepath.ToSlash(filepath.Clean(apiErr.Details().GetString("path", ""))), cleanPath)) {
		return
	}
	t.Fatalf("expected error to identify path %q, got: %v", path, err)
}

func assertPathNotFound(t *testing.T, err error, module, path string) {
	t.Helper()
	require.Error(t, err)
	assertErrorIdentifiesModule(t, err, module)
	assertErrorIdentifiesPath(t, err, path)
	var apiErr apierror.Error
	if errors.As(err, &apiErr) {
		require.Equal(t, apierror.Invalid, apiErr.Kind())
	}
	require.True(t, errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist),
		"expected error to satisfy errors.Is(fs.ErrNotExist), got: %v", err)
}

type testFixture struct {
	root            string
	lockPath        string
	vendorDir       string
	replacementPath string
	replacements    []lock.Replacement
}

func newTestFixture(t *testing.T) *testFixture {
	t.Helper()
	root := t.TempDir()
	f := &testFixture{
		root:            root,
		lockPath:        filepath.Join(root, lock.DefaultFilename),
		vendorDir:       filepath.Join(root, "vendor"),
		replacementPath: filepath.Join(root, "local-mod"),
	}
	f.replacements = []lock.Replacement{{From: "local/mod", To: f.replacementPath}}
	f.writeIndex(t, "1")
	return f
}

func (f *testFixture) writeIndex(t *testing.T, gen any) {
	t.Helper()
	require.NoError(t, os.MkdirAll(f.replacementPath, 0o755))
	content := fmt.Sprintf(`{"namespace":"local.mod","entries":[{"name":"svc","kind":"registry.entry","data":{"generation":%q}}]}`, fmt.Sprint(gen))
	require.NoError(t, os.WriteFile(filepath.Join(f.replacementPath, "_index.json"), []byte(content), 0o600))
}

func (f *testFixture) newHandler(t *testing.T, replacements []lock.Replacement) *DependencyHandler {
	t.Helper()
	h, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:                   &fakeHub{},
		Logger:                zap.NewNop(),
		LockPath:              f.lockPath,
		VendorDir:             f.vendorDir,
		WorkspaceReplacements: replacements,
	})
	require.NoError(t, err)
	return h
}

func (f *testFixture) newHistoryWithModule(t *testing.T, source, digest string, size uint64) (*historymem.Storage, regapi.DependencyResolution) {
	t.Helper()
	mod := regapi.ResolvedModule{
		Name: "local/mod", Version: "1.0.0", VersionID: "1.0.0",
		Source: source, Digest: digest, SizeBytes: size,
	}
	res := dependencyResolution([]desiredDependency{{
		entry:      hardeningRoot("app:mod", "local/mod", "1.0.0"),
		definition: DependencyDefinition{Component: "local/mod", Version: "1.0.0"},
	}}, nil, []ResolvedModule{{
		Org: "local", Name: "mod", Version: "1.0.0",
		Source: source, Digest: digest, SizeBytes: size,
	}})
	res.Deployment = (&regapi.Deployment{Root: "local/mod", Modules: []regapi.ResolvedModule{mod}}).Canonical()
	res = res.Canonical()
	return restoreHistoryWithResolution(t, res), *res
}

func (f *testFixture) writeLock(t *testing.T, version, hash string) *lock.Lock {
	t.Helper()
	l, err := lock.New(f.lockPath, lock.WithWorkspaceReplacements(f.replacements))
	require.NoError(t, err)
	l.SetDirectories(lock.Directories{Modules: "vendor", Src: "./src"})
	if version != "" {
		l.SetModule(lock.Module{Name: "local/mod", Version: version, Hash: hash})
	}
	require.NoError(t, l.Write())
	return l
}

// TestReplacementRestore_BoundaryCases recovers and elevates review boundary cases
// into a cross-platform table-driven suite asserting structured API diagnostics.
func TestReplacementRestore_BoundaryCases(t *testing.T) {
	symlinksSupported := supportsReplacementSymlinks(t)
	tests := []struct {
		name                 string
		source               string
		pathKind             string // "dir", "missing", "file", "symlink"
		recordedDigestKind   string // "stale", "broken", "valid-hub"
		expectedErrSubstring string
		configured           bool
		wantError            bool
	}{
		{
			name:               "edited-local-replacement",
			source:             moduleSourceReplacementTreeV1,
			pathKind:           "dir",
			configured:         true,
			recordedDigestKind: "stale",
			wantError:          false,
		},
		{
			name:                 "unconfigured-workspace-replacement",
			source:               moduleSourceReplacementTreeV1,
			pathKind:             "dir",
			configured:           false,
			wantError:            true,
			expectedErrSubstring: "stored local replacement is not configured",
		},
		{
			name:       "missing-replacement-path",
			source:     moduleSourceReplacementTreeV1,
			pathKind:   "missing",
			configured: true,
			wantError:  true,
		},
		{
			name:                 "file-as-replacement-path",
			source:               moduleSourceReplacementTreeV1,
			pathKind:             "file",
			configured:           true,
			wantError:            true,
			expectedErrSubstring: "replacement path is not a directory",
		},
		{
			name:                 "symlink-replacement-path",
			source:               moduleSourceReplacementTreeV1,
			pathKind:             "symlink",
			configured:           true,
			wantError:            true,
			expectedErrSubstring: "module tree contains symlink",
		},
		{
			name:                 "malformed-digest",
			source:               moduleSourceReplacementTreeV1,
			pathKind:             "dir",
			configured:           true,
			recordedDigestKind:   "broken",
			wantError:            true,
			expectedErrSubstring: "invalid sha256-tree-v1 digest for local/mod@1.0.0",
		},
		{
			name:               "hub-source-overridden-by-configured-replacement",
			source:             moduleSourceHub,
			pathKind:           "dir",
			configured:         true,
			recordedDigestKind: "valid-hub",
			wantError:          false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.pathKind == "symlink" && !symlinksSupported {
				t.Skip("skipping symlink test: symlinks not supported on this platform/environment")
			}

			f := newTestFixture(t)
			require.NoError(t, os.WriteFile(f.lockPath, []byte("directories:\n  modules: .wippy\n  src: ./src\n"), 0o600))

			switch tc.pathKind {
			case "missing":
				require.NoError(t, os.RemoveAll(f.replacementPath))
			case "file":
				require.NoError(t, os.RemoveAll(f.replacementPath))
				require.NoError(t, os.WriteFile(f.replacementPath, []byte("regular file"), 0o600))
			case "symlink":
				target := filepath.Join(f.root, "target")
				require.NoError(t, os.MkdirAll(target, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(target, "_index.json"), []byte(`{"namespace":"local.mod","entries":[]}`), 0o600))
				require.NoError(t, os.RemoveAll(f.replacementPath))
				require.NoError(t, os.Symlink(target, f.replacementPath))
			}

			digest := "sha256-tree-v1:" + strings.Repeat("a", 64)
			var size uint64 = 10
			switch tc.recordedDigestKind {
			case "stale":
				digest = "sha256-tree-v1:" + strings.Repeat("e", 64)
				if tc.pathKind == "dir" {
					require.NoError(t, os.WriteFile(filepath.Join(f.replacementPath, "extra.txt"), []byte("extra"), 0o600))
				}
			case "broken":
				digest = "broken"
			case "valid-hub":
				digest = "sha256:" + strings.Repeat("c", 64)
				size = 25
			}

			history, beforeRes := f.newHistoryWithModule(t, tc.source, digest, size)
			head, err := history.Head()
			require.NoError(t, err)

			var replacements []lock.Replacement
			if tc.configured {
				replacements = f.replacements
			}

			handler := f.newHandler(t, replacements)
			sources := moduleapi.NewSourceRegistry()
			ctx := moduleapi.WithSourceRegistry(newTestContext(), sources)

			restoreErr := handler.PrepareRestore(ctx, history)
			if tc.wantError {
				require.Error(t, restoreErr)
				if tc.pathKind == "missing" {
					assertPathNotFound(t, restoreErr, "local/mod", f.replacementPath)
				} else {
					if tc.expectedErrSubstring != "" {
						require.Contains(t, restoreErr.Error(), tc.expectedErrSubstring)
					}
					assertErrorIdentifiesModule(t, restoreErr, "local/mod")
					if tc.pathKind == "file" {
						assertErrorIdentifiesPath(t, restoreErr, f.replacementPath)
					}
				}
			} else {
				require.NoError(t, restoreErr)
				src, ok := sources.Snapshot()["local/mod"]
				require.True(t, ok)
				require.True(t, src.Replacement)
				require.Equal(t, filepath.Clean(f.replacementPath), filepath.Clean(src.LoadPath))

				expectedDiskDigest, _, err := digestReplacementTree(f.replacementPath)
				require.NoError(t, err)
				require.Equal(t, expectedDiskDigest, src.Digest)
			}

			afterRes, err := history.GetDependencyResolution(head)
			require.NoError(t, err)
			require.Equal(t, beforeRes.Digest, afterRes.Digest)
			require.Equal(t, beforeRes.Modules[0], afterRes.Modules[0])
		})
	}
}

// TestReplacementRestore_MutationAfterIdentityRefresh verifies that mutating replacement
// content after identity snapshot fails closed with a digest mismatch error.
func TestReplacementRestore_MutationAfterIdentityRefresh(t *testing.T) {
	ctx := newTestContext()
	f := newTestFixture(t)
	dataFile := filepath.Join(f.replacementPath, "file.txt")
	require.NoError(t, os.WriteFile(dataFile, []byte("gen-1"), 0o600))

	handler := &DependencyHandler{
		replacements: map[string]lock.Replacement{"local/mod": f.replacements[0]},
		vendorDir:    t.TempDir(),
	}
	modules := []ResolvedModule{{Org: "local", Name: "mod", Version: "1.0.0"}}

	require.NoError(t, handler.refreshReplacementModuleIdentities(modules))
	require.NotEmpty(t, modules[0].Digest)

	require.NoError(t, os.WriteFile(dataFile, []byte("gen-2"), 0o600))

	_, err := handler.ensureModuleAvailable(ctx, modules[0])
	require.Error(t, err)
	var apiErr apierror.Error
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, apierror.Invalid, apiErr.Kind())
	assertErrorIdentifiesModule(t, err, "local/mod")
	require.Contains(t, err.Error(), "replacement content digest mismatch")
}

// TestReplacementRestore_LockAndCacheLifecycle verifies that workspace replacement
// overlays are independent of wippy.lock, resolution handles modules absent/present,
// lock regeneration selects replacement load paths, cache wipes recover cleanly,
// and only configuration removal yields unconfigured errors.
func TestReplacementRestore_LockAndCacheLifecycle(t *testing.T) {
	ctx := newTestContext()
	f := newTestFixture(t)
	roots := []DependencyDefinition{{Component: "local/mod", Version: "*"}}

	// 1. Fresh onboarding state: history has no versions. PrepareRestore is clean no-op.
	handlerFresh := f.newHandler(t, f.replacements)
	require.NoError(t, handlerFresh.PrepareRestore(ctx, historymem.New()))

	// 2. Replacement resolution with module absent from lock modules (only directories).
	require.NoError(t, os.WriteFile(f.lockPath, []byte("directories:\n  modules: vendor\n  src: ./src\n"), 0o600))
	handlerAbsent := f.newHandler(t, f.replacements)
	resolvedAbsent, err := handlerAbsent.ResolveWorkspaceDependencies(ctx, roots)
	require.NoError(t, err)
	require.Len(t, resolvedAbsent, 1)
	require.Equal(t, replacementZeroVersion, resolvedAbsent[0].Version,
		"absent lock module should resolve to replacementZeroVersion (0.0.0)")

	// 3. Reconstruct lock and assert GetModuleLoadPaths selects replacement
	reconstructedLock := f.writeLock(t, resolvedAbsent[0].Version, resolvedAbsent[0].Digest)
	found := false
	for _, lp := range reconstructedLock.GetModuleLoadPaths() {
		if lp.Module == "local/mod" && lp.Replacement && filepath.Clean(lp.Path) == filepath.Clean(f.replacementPath) {
			found = true
			break
		}
	}
	require.True(t, found, "reconstructed lock must select replacement path")

	// 4. Replacement resolution with module pinned in lock modules.
	f.writeLock(t, "1.2.3", strings.Repeat("a", 64))
	handlerPinned := f.newHandler(t, f.replacements)
	resolvedPinned, err := handlerPinned.ResolveWorkspaceDependencies(ctx, roots)
	require.NoError(t, err)
	require.Equal(t, "1.2.3", resolvedPinned[0].Version)

	// 5. Restore succeeds with history
	recordedDigest, recordedSize, err := digestReplacementTree(f.replacementPath)
	require.NoError(t, err)
	history, _ := f.newHistoryWithModule(t, moduleSourceReplacementTreeV1, recordedDigest, recordedSize)
	require.NoError(t, handlerPinned.PrepareRestore(ctx, history))

	// 6. Lock deletion: configured replacement continues to restore (orthogonal overlay)
	require.NoError(t, os.Remove(f.lockPath))
	handlerNoLock := f.newHandler(t, f.replacements)
	require.NoError(t, handlerNoLock.PrepareRestore(ctx, history),
		"configured workspace replacement must restore successfully even when wippy.lock is deleted")

	// 7. Cold cache wipe: vendor directory removed, handler recreates and restores cleanly
	require.NoError(t, os.RemoveAll(f.vendorDir))
	handlerWipedCache := f.newHandler(t, f.replacements)
	require.NoError(t, handlerWipedCache.PrepareRestore(ctx, history),
		"replacement restores directly from local tree, unaffected by vendor cache wipe")
	stat, err := os.Stat(f.vendorDir)
	require.NoError(t, err)
	require.True(t, stat.IsDir())

	// 8. Only explicit configuration removal produces the unconfigured error
	handlerUnconfigured := f.newHandler(t, nil)
	err = handlerUnconfigured.PrepareRestore(ctx, history)
	require.Error(t, err)
	require.Contains(t, err.Error(), "stored local replacement is not configured")
	assertErrorIdentifiesModule(t, err, "local/mod")
}

// TestReplacementRestore_HistoryUndoRedoAndColdRestart models real dependency
// install, delete, undo, redo, and cold restart boundaries with SQLite history.
func TestReplacementRestore_HistoryUndoRedoAndColdRestart(t *testing.T) {
	ctx := newTestContext()
	f := newTestFixture(t)
	dbPath := filepath.Join(f.root, "registry.db")
	f.writeLock(t, "", "")

	hubCalls := 0
	offlineHub := &fakeHub{
		getManifest: func(context.Context, string, string, string) (*ModuleManifest, error) {
			hubCalls++
			return nil, errors.New("offline hub: unexpected network call")
		},
		getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
			hubCalls++
			return nil, errors.New("offline hub: unexpected network call")
		},
	}

	newHandler := func() *DependencyHandler {
		h, err := NewDependencyHandler(DependencyHandlerOptions{
			Hub:                   offlineHub,
			Logger:                zap.NewNop(),
			LockPath:              f.lockPath,
			VendorDir:             f.vendorDir,
			WorkspaceReplacements: f.replacements,
		})
		require.NoError(t, err)
		return h
	}

	newRegistry := func(h regapi.History, hdl *DependencyHandler) *registryimpl.Reg {
		res := topology.NewResolver()
		hdl.resolver = res
		return registryimpl.NewRegistry(h, &bootRecordingRunner{}, topology.NewStateBuilder(zap.NewNop(), res), res, zap.NewNop(),
			registryimpl.WithKindDirective(regapi.NamespaceDependency,
				expansion.NewDependencyDirective(hdl.Expand).
					WithResolutionTransition(hdl.ReconcileResolution).
					WithChangesExpansion(hdl.ExpandChanges)))
	}

	history, err := historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	reg := newRegistry(history, newHandler())
	regCtx := regapi.WithRegistry(ctx, reg)

	// Step 1: Install real ns.dependency
	rootEntry := regapi.Entry{
		ID:   regapi.NewID("app.deps", "local"),
		Kind: regapi.NamespaceDependency,
		Data: payload.New(map[string]any{"component": "local/mod", "version": "v0.1.0"}),
	}
	v1, err := reg.Apply(regCtx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: rootEntry}})
	require.NoError(t, err)
	require.Zero(t, hubCalls)

	svcEntry, err := reg.GetEntry(regapi.NewID("local.mod", "svc"))
	require.NoError(t, err)
	require.Equal(t, "1", svcEntry.Data.Data().(map[string]any)["generation"])

	v1Res, err := history.GetDependencyResolution(v1)
	require.NoError(t, err)
	require.Len(t, v1Res.Modules, 1)
	v1Digest := v1Res.Modules[0].Digest

	// Step 2: Delete the real ns.dependency
	v2, err := reg.Apply(regCtx, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: rootEntry.ID}}})
	require.NoError(t, err)
	_, err = reg.GetEntry(regapi.NewID("local.mod", "svc"))
	require.Error(t, err)
	v2Res, err := history.GetDependencyResolution(v2)
	require.NoError(t, err)
	require.Empty(t, v2Res.Modules, "deleting the dependency must remove its selected module graph")

	// Step 3: Mutate replacement tree on disk to gen-2
	f.writeIndex(t, "2")

	// Step 4: Undo to V1 (restores dependency with gen-2 edited tree)
	require.NoError(t, reg.ApplyVersion(regCtx, v1))
	require.Zero(t, hubCalls)

	svcAfterUndo, err := reg.GetEntry(regapi.NewID("local.mod", "svc"))
	require.NoError(t, err)
	require.Equal(t, "2", svcAfterUndo.Data.Data().(map[string]any)["generation"])

	v1ResAfterUndo, err := history.GetDependencyResolution(v1)
	require.NoError(t, err)
	require.Equal(t, v1Digest, v1ResAfterUndo.Modules[0].Digest, "checkpoint must remain immutable")

	// Step 5: Redo to V2 (removes dependency again)
	require.NoError(t, reg.ApplyVersion(regCtx, v2))
	require.Zero(t, hubCalls)
	_, err = reg.GetEntry(regapi.NewID("local.mod", "svc"))
	require.Error(t, err)

	// Step 6: Cold Restart Boundary (explicitly close SQLite db before reopening)
	require.NoError(t, history.Close())
	f.writeIndex(t, "3")

	restartedHistory, err := historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = restartedHistory.Close() })

	restartedHandler := newHandler()
	restartCtx := moduleapi.WithSourceRegistry(ctx, moduleapi.NewSourceRegistry())
	require.NoError(t, restartedHandler.PrepareRestore(restartCtx, restartedHistory))
	require.Zero(t, hubCalls)

	restartedReg := newRegistry(restartedHistory, restartedHandler)
	restartRegCtx := regapi.WithRegistry(restartCtx, restartedReg)
	require.NoError(t, restartedReg.LoadState(restartRegCtx, nil, v2))
	_, err = restartedReg.GetEntry(regapi.NewID("local.mod", "svc"))
	require.Error(t, err)

	// Step 7: History navigation after cold restart: undo to V1 restores with gen-3
	require.NoError(t, restartedReg.ApplyVersion(restartRegCtx, v1))
	restartedSvcV1, err := restartedReg.GetEntry(regapi.NewID("local.mod", "svc"))
	require.NoError(t, err)
	require.Equal(t, "3", restartedSvcV1.Data.Data().(map[string]any)["generation"])

	// Redo to V2 removes it again
	require.NoError(t, restartedReg.ApplyVersion(restartRegCtx, v2))
	_, err = restartedReg.GetEntry(regapi.NewID("local.mod", "svc"))
	require.Error(t, err)

	// Checkpoint resolution in SQLite remains unmodified
	v1ResRestart, err := restartedHistory.GetDependencyResolution(v1)
	require.NoError(t, err)
	require.Equal(t, v1Digest, v1ResRestart.Modules[0].Digest)
}

// TestReplacementRestore_DeterministicStateMachine exercises randomized sequences
// of lifecycle operations ensuring orthogonality and cross-platform error safety.
func TestReplacementRestore_DeterministicStateMachine(t *testing.T) {
	symlinksSupported := supportsReplacementSymlinks(t)
	for seed := int64(0); seed < 10; seed++ {
		t.Run(fmt.Sprintf("seed-%02d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			f := newTestFixture(t)

			generation := 1
			workspaceConfigured := true
			lockFilePresent := true
			pathMode := "dir" // "dir", "missing", "file", "symlink"

			applyPathMode := func() {
				_ = os.RemoveAll(f.replacementPath)
				switch pathMode {
				case "dir":
					f.writeIndex(t, generation)
				case "file":
					require.NoError(t, os.WriteFile(f.replacementPath, []byte("file-content"), 0o600))
				case "symlink":
					if !symlinksSupported {
						pathMode = "dir"
						f.writeIndex(t, generation)
						return
					}
					f.writeIndex(t, generation)
					idx := filepath.Join(f.replacementPath, "_index.json")
					require.NoError(t, os.Symlink(idx, filepath.Join(f.replacementPath, "link.txt")))
				case "missing":
					// left absent
				}
			}
			applyPathMode()

			syncLock := func() {
				if !lockFilePresent {
					_ = os.Remove(f.lockPath)
					return
				}
				require.NoError(t, os.WriteFile(f.lockPath, []byte("directories:\n  modules: vendor\n  src: ./src\n"), 0o600))
			}
			syncLock()

			recordedDigest, recordedSize, err := digestReplacementTree(f.replacementPath)
			require.NoError(t, err)
			history, _ := f.newHistoryWithModule(t, moduleSourceReplacementTreeV1, recordedDigest, recordedSize)

			trace := make([]string, 0, 32)
			fail := func(step int, format string, args ...any) {
				t.Helper()
				t.Fatalf("seed=%d step=%d: %s\ntrace:\n  %s",
					seed, step, fmt.Sprintf(format, args...), strings.Join(trace, "\n  "))
			}

			for step := 0; step < 25; step++ {
				op := rng.Intn(7)
				switch op {
				case 0:
					generation++
					applyPathMode()
					trace = append(trace, fmt.Sprintf("%02d edit content -> gen %d", step, generation))
				case 1:
					workspaceConfigured = !workspaceConfigured
					trace = append(trace, fmt.Sprintf("%02d toggle config -> %t", step, workspaceConfigured))
				case 2:
					switch pathMode {
					case "dir":
						pathMode = "missing"
					case "missing":
						pathMode = "file"
					case "file":
						if symlinksSupported {
							pathMode = "symlink"
						} else {
							pathMode = "dir"
						}
					case "symlink":
						pathMode = "dir"
					}
					applyPathMode()
					trace = append(trace, fmt.Sprintf("%02d path mode -> %s", step, pathMode))
				case 3:
					pathMode = "dir"
					applyPathMode()
					trace = append(trace, fmt.Sprintf("%02d reset path mode -> dir", step))
				case 4:
					lockFilePresent = !lockFilePresent
					syncLock()
					trace = append(trace, fmt.Sprintf("%02d toggle lock -> %t", step, lockFilePresent))
				case 5:
					_ = os.RemoveAll(f.vendorDir)
					trace = append(trace, fmt.Sprintf("%02d wipe vendor cache", step))
				case 6:
					trace = append(trace, fmt.Sprintf("%02d test PrepareRestore", step))
				}

				var replacements []lock.Replacement
				if workspaceConfigured {
					replacements = f.replacements
				}

				handler := f.newHandler(t, replacements)
				sources := moduleapi.NewSourceRegistry()
				ctx := moduleapi.WithSourceRegistry(newTestContext(), sources)

				err := handler.PrepareRestore(ctx, history)

				if !workspaceConfigured {
					if err == nil || !strings.Contains(err.Error(), "stored local replacement is not configured") {
						fail(step, "expected unconfigured replacement error, got: %v", err)
					}
					assertErrorIdentifiesModule(t, err, "local/mod")
					continue
				}

				switch pathMode {
				case "missing":
					assertPathNotFound(t, err, "local/mod", f.replacementPath)

				case "file":
					if err == nil || !strings.Contains(err.Error(), "replacement path is not a directory") {
						fail(step, "expected non-directory error, got: %v", err)
					}
					assertErrorIdentifiesModule(t, err, "local/mod")
					assertErrorIdentifiesPath(t, err, f.replacementPath)

				case "symlink":
					if err == nil || !strings.Contains(err.Error(), "module tree contains symlink") {
						fail(step, "expected symlink error, got: %v", err)
					}
					assertErrorIdentifiesModule(t, err, "local/mod")

				case "dir":
					if err != nil {
						fail(step, "valid replacement restore failed: %v", err)
					}
					src := sources.Snapshot()["local/mod"]
					if !src.Replacement || filepath.Clean(src.LoadPath) != filepath.Clean(f.replacementPath) {
						fail(step, "unexpected source registry snapshot: %+v", src)
					}
					currentDigest, _, digestErr := digestReplacementTree(f.replacementPath)
					if digestErr != nil || src.Digest != currentDigest {
						fail(step, "source digest = %s; want %s (err: %v)", src.Digest, currentDigest, digestErr)
					}
				}
			}
		})
	}
}
