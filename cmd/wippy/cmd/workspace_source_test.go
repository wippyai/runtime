// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	bootapi "github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

type hostDependencyWorkspace struct {
	cfg      bootapi.Config
	root     string
	source   string
	app      string
	guide    string
	lockPath string
}

func newHostDependencyWorkspace(t *testing.T, enabled bool) *hostDependencyWorkspace {
	t.Helper()
	root := t.TempDir()
	w := &hostDependencyWorkspace{
		root: root, source: filepath.Join(root, "src"), app: filepath.Join(root, "app"),
		guide: filepath.Join(root, "guide"), lockPath: filepath.Join(root, "wippy.lock"),
	}
	for _, path := range []string{w.source, filepath.Join(w.app, "src"), filepath.Join(w.guide, "src")} {
		require.NoError(t, os.MkdirAll(path, 0o755))
	}
	w.writeHost(t, true)
	require.NoError(t, os.WriteFile(filepath.Join(w.app, "src", "_index.yaml"), []byte(`namespace: acme.app
entries:
  - name: entry
    kind: registry.entry
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(w.guide, "src", "_index.yaml"), []byte(`namespace: local.guide
entries:
  - name: entry
    kind: registry.entry
`), 0o600))
	w.cfg = bootapi.NewConfig(bootapi.WithSection("workspace", map[string]any{
		"options.include_source_dependencies": enabled,
		"replacements.acme/app":               w.app,
		"replacements.local/guide":            w.guide,
	}))
	locked, err := lock.New(w.lockPath)
	require.NoError(t, err)
	locked.SetDirectories(lock.Directories{Src: "src", Modules: ".wippy"})
	locked.SetModule(lock.Module{Name: "acme/app", Version: "1.0.0", Root: true})
	require.NoError(t, locked.Write())
	return w
}

func (w *hostDependencyWorkspace) writeHost(t *testing.T, guide bool) {
	t.Helper()
	body := "namespace: host.deps\nentries: []\n"
	if guide {
		body = `namespace: host.deps
entries:
  - name: guide
    kind: ns.dependency
    component: local/guide
    version: "*"
`
	}
	require.NoError(t, os.WriteFile(filepath.Join(w.source, "_index.yaml"), []byte(body), 0o600))
}

func (w *hostDependencyWorkspace) configuredLock(t *testing.T) *lock.Lock {
	t.Helper()
	locked, err := newConfiguredLock(w.lockPath, w.cfg, zap.NewNop())
	require.NoError(t, err)
	return locked
}

func (w *hostDependencyWorkspace) roots(ctx context.Context, t *testing.T) []dependencyRequest {
	t.Helper()
	requests, err := loadUpdateRoots(ctx, bootapi.GetLoader(ctx), w.source, w.configuredLock(t), payload.GetTranscoder(ctx), zap.NewNop(), w.cfg)
	require.NoError(t, err)
	return requests
}

func TestUpdateRootedWorkspaceIncludesHostDependenciesOnlyWhenEnabled(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "enabled"}[enabled], func(t *testing.T) {
			w := newHostDependencyWorkspace(t, enabled)
			ctx := setupLoaderContext(t)
			requests := w.roots(ctx, t)
			want := []dependencyRequest{{Org: "acme", Module: "app"}}
			if enabled {
				want = append(want, dependencyRequest{Org: "local", Module: "guide", Constraint: "*"})
			}
			require.ElementsMatch(t, want, requests)
		})
	}
}

func TestUpdateRootedWorkspaceSelectsUnpublishedGuideAndPreservesAppIdentity(t *testing.T) {
	w := newHostDependencyWorkspace(t, true)
	ctx := setupLoaderContext(t)
	appManifest := hub.ModuleManifest{Org: "acme", Name: "app", Version: "1.0.0",
		Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	provider := runManifestProvider{manifests: map[string]hub.ModuleManifest{
		"acme/app@": appManifest, "acme/app@1.0.0": appManifest,
	}}
	resolved, err := resolveUpdatedWorkspaceDependencies(ctx, provider, w.configuredLock(t), w.lockPath, w.cfg, w.roots(ctx, t), nil)
	require.NoError(t, err, "the provider has no local/guide publication")
	require.ElementsMatch(t, []hub.ResolvedModule{
		{Org: "acme", Name: "app", Version: "1.0.0"},
		{Org: "local", Name: "guide", Version: "0.0.0"},
	}, resolved)
	regenerated, err := convertResolvedToLock(w.lockPath, resolved, ".wippy", "src")
	require.NoError(t, err)
	require.Equal(t, []string{"acme/app"}, regenerated.GetRootModules())
	require.NoError(t, regenerated.Write())
	paths := w.configuredLock(t).GetModuleLoadPaths()
	require.Len(t, paths, 3)
	require.Equal(t, filepath.Join(w.guide, "src"), paths[2].Path)
	require.False(t, paths[2].Root, "a host dependency is not a second application root")
}

func TestPrepareRootedWorkspaceCompletesLocalGraphAndRestartsOffline(t *testing.T) {
	w := newHostDependencyWorkspace(t, true)
	t.Chdir(w.root)
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(rw, "unexpected Hub request", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	for range 2 {
		require.NoError(t, prepareRunDependencies(setupLoaderContext(t), w.cfg, server.URL, zap.NewNop()))
		locked := w.configuredLock(t)
		require.Equal(t, []string{"acme/app"}, locked.GetRootModules())
		app, ok := locked.GetModule("acme/app")
		require.True(t, ok)
		require.Equal(t, "1.0.0", app.Version, "startup must not upgrade the selected application")
		guide, ok := locked.GetModule("local/guide")
		require.True(t, ok)
		require.Equal(t, "0.0.0", guide.Version)
	}
	require.Zero(t, calls.Load(), "local completion and restart must not call the Hub")
	before, err := os.ReadFile(w.lockPath)
	require.NoError(t, err)
	require.NoError(t, prepareRunDependencies(setupLoaderContext(t), w.cfg, server.URL, zap.NewNop()))
	after, err := os.ReadFile(w.lockPath)
	require.NoError(t, err)
	require.Equal(t, before, after, "a complete workspace must not rewrite the lock on restart")
	server.Close()
	require.NoError(t, prepareRunDependencies(setupLoaderContext(t), w.cfg, server.URL, zap.NewNop()), "complete graphs restart with an unreachable Hub")
}

func TestPrepareRootedWorkspaceDefaultLeavesLockAndMissingSourceAlone(t *testing.T) {
	w := newHostDependencyWorkspace(t, false)
	t.Chdir(w.root)
	require.NoError(t, os.Rename(w.source, w.source+".hidden"))
	before, err := os.ReadFile(w.lockPath)
	require.NoError(t, err)
	require.NoError(t, prepareRunDependencies(setupLoaderContext(t), w.cfg, "http://127.0.0.1:1", zap.NewNop()))
	after, err := os.ReadFile(w.lockPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestPrepareRootedWorkspacePrunesRemovedHostDependency(t *testing.T) {
	w := newHostDependencyWorkspace(t, true)
	t.Chdir(w.root)
	require.NoError(t, prepareRunDependencies(setupLoaderContext(t), w.cfg, "http://127.0.0.1:1", zap.NewNop()))
	w.writeHost(t, false)
	require.NoError(t, prepareRunDependencies(setupLoaderContext(t), w.cfg, "http://127.0.0.1:1", zap.NewNop()))
	locked := w.configuredLock(t)
	_, selected := locked.GetModule("local/guide")
	require.False(t, selected, "removed, unreferenced host dependencies must not survive merely because they are replacements")
	require.Equal(t, []string{"acme/app"}, locked.GetRootModules())
}

func TestPrepareRootedWorkspaceRetainsDependencyStillRequiredByApp(t *testing.T) {
	w := newHostDependencyWorkspace(t, true)
	t.Chdir(w.root)
	require.NoError(t, os.WriteFile(filepath.Join(w.app, "src", "_index.yaml"), []byte(`namespace: acme.app
entries:
  - name: guide
    kind: ns.dependency
    component: local/guide
    version: "*"
`), 0o600))
	require.NoError(t, prepareRunDependencies(setupLoaderContext(t), w.cfg, "http://127.0.0.1:1", zap.NewNop()))
	w.writeHost(t, false)
	require.NoError(t, prepareRunDependencies(setupLoaderContext(t), w.cfg, "http://127.0.0.1:1", zap.NewNop()))
	_, selected := w.configuredLock(t).GetModule("local/guide")
	require.True(t, selected, "removing the host reference must not prune an application dependency")
}

func TestPrepareRootedWorkspaceDoesNotDiscoverParentLock(t *testing.T) {
	w := newHostDependencyWorkspace(t, true)
	before, err := os.ReadFile(w.lockPath)
	require.NoError(t, err)
	t.Chdir(w.app)
	require.NoError(t, prepareRunDependencies(setupLoaderContext(t), w.cfg, "http://127.0.0.1:1", zap.NewNop()))
	after, err := os.ReadFile(w.lockPath)
	require.NoError(t, err)
	require.Equal(t, before, after, "opting in must not broaden lock discovery into parent directories")
}

func TestPrepareRootedWorkspaceConflictingHostConstraintLeavesLockUnchanged(t *testing.T) {
	w := newHostDependencyWorkspace(t, true)
	t.Chdir(w.root)
	require.NoError(t, os.WriteFile(filepath.Join(w.source, "_index.yaml"), []byte(`namespace: host.deps
entries:
  - name: app
    kind: ns.dependency
    component: acme/app
    version: "<1.0.0"
`), 0o600))
	before, err := os.ReadFile(w.lockPath)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		http.Error(rw, "no compatible version", http.StatusBadRequest)
	}))
	defer server.Close()
	err = prepareRunDependencies(setupLoaderContext(t), w.cfg, server.URL, zap.NewNop())
	require.ErrorContains(t, err, "acme/app")
	after, err := os.ReadFile(w.lockPath)
	require.NoError(t, err)
	require.Equal(t, before, after, "startup must neither upgrade the app nor save a conflicting graph")
}

func TestPrepareRootedWorkspaceAddsLocalGuideToCachedAppOffline(t *testing.T) {
	w := newHostDependencyWorkspace(t, true)
	t.Chdir(w.root)
	w.cfg = bootapi.NewConfig(bootapi.WithSection("workspace", map[string]any{
		"options.include_source_dependencies": true,
		"replacements.local/guide":            w.guide,
	}))
	path := filepath.Join(w.root, w.configuredLock(t).GetVendorPath(), "acme", "app-1.0.0.wapp")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	data := writeSealedPack(t, path, wapp.Metadata{"name": "app", "namespace": "acme.app", "version": "1.0.0"},
		wapp.Entry{ID: wapp.NewID("acme.app", "definition"), Kind: "ns.definition"})
	sum := sha256.Sum256(data)
	locked := w.configuredLock(t)
	locked.SetModule(lock.Module{Name: "acme/app", Version: "1.0.0", Root: true, Hash: hex.EncodeToString(sum[:])})
	require.NoError(t, locked.Write())
	ctx := setupLoaderContext(t)
	requests, err := loadWorkspaceRoots(ctx, bootapi.GetLoader(ctx), w.source, locked, payload.GetTranscoder(ctx), zap.NewNop(), w.cfg)
	require.NoError(t, err)
	_, err = resolveRunDependencies(regapi.WithDependencyAccess(ctx, regapi.DependencyAccessVerifiedOffline), runManifestProvider{}, locked, requests)
	require.NoError(t, err, "cached graph is independently verifiable offline")
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(rw, "unexpected Hub request", http.StatusBadRequest)
	}))
	defer server.Close()
	require.NoError(t, prepareRunDependencies(setupLoaderContext(t), w.cfg, server.URL, zap.NewNop()))
	require.Zero(t, calls.Load(), "a cached app plus a new local guide can complete without contacting the Hub")
	selected := w.configuredLock(t)
	app, ok := selected.GetModule("acme/app")
	require.True(t, ok)
	require.Equal(t, "1.0.0", app.Version)
	require.Equal(t, hex.EncodeToString(sum[:]), app.Hash)
	_, ok = selected.GetModule("local/guide")
	require.True(t, ok)
	server.Close()
	require.NoError(t, prepareRunDependencies(setupLoaderContext(t), w.cfg, server.URL, zap.NewNop()))
}

func TestIncludeSourceDependenciesRequiresExplicitBoolean(t *testing.T) {
	for _, tc := range []struct {
		value any
		name  string
		want  bool
		valid bool
	}{
		{name: "disabled", value: false, valid: true},
		{name: "enabled", value: true, want: true, valid: true},
		{name: "reset", value: nil, valid: true},
		{name: "string", value: "true"},
		{name: "number", value: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := bootapi.NewConfig(bootapi.WithSection("workspace", map[string]any{
				"options.include_source_dependencies": tc.value,
			}))
			got, err := includeSourceDependencies(cfg)
			if !tc.valid {
				require.ErrorContains(t, err, includeSourceDependenciesKey)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
	for _, cfg := range []bootapi.Config{nil, bootapi.NewConfig()} {
		got, err := includeSourceDependencies(cfg)
		require.NoError(t, err)
		require.False(t, got)
	}
}

func TestHostDependencyOptionComposesThroughProfilesAndSet(t *testing.T) {
	resetRuntimeFlagGlobals(t)
	path := filepath.Join(t.TempDir(), ".wippy.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`version: "1.0"
profiles:
  development:
    workspace:
      options:
        include_source_dependencies: true
  isolated:
    workspace:
      options:
        include_source_dependencies: false
`), 0o600))
	setTestConfigFiles(t, path)
	for _, tc := range []struct {
		name     string
		profiles []string
		sets     []string
		want     bool
	}{
		{name: "default"},
		{name: "profile", profiles: []string{"development"}, want: true},
		{name: "last-profile", profiles: []string{"development", "isolated"}},
		{name: "set", profiles: []string{"development"}, sets: []string{includeSourceDependenciesKey + "=false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadRuntimeConfig(runtimeConfigCommand(t, tc.profiles, tc.sets), zap.NewNop())
			require.NoError(t, err)
			got, err := includeSourceDependencies(cfg)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestWorkspaceRootDiscoveryDoesNotEnrollUnusedReplacement(t *testing.T) {
	w := newHostDependencyWorkspace(t, true)
	w.writeHost(t, false)
	require.NoError(t, os.Rename(w.guide, w.guide+".hidden"))
	requests := w.roots(setupLoaderContext(t), t)
	require.Equal(t, []dependencyRequest{{Org: "acme", Module: "app"}}, requests)
}

func TestWorkspaceRootDiscoveryKeepsSourceOnlyBehavior(t *testing.T) {
	w := newHostDependencyWorkspace(t, false)
	locked := w.configuredLock(t)
	locked.SetRootModule("")
	require.NoError(t, locked.Write())
	requests := w.roots(setupLoaderContext(t), t)
	require.Equal(t, []dependencyRequest{{Org: "local", Module: "guide", Constraint: "*"}}, requests)
}

func TestWorkspaceRootDiscoveryPreservesHostConstraintOnApplication(t *testing.T) {
	w := newHostDependencyWorkspace(t, true)
	require.NoError(t, os.WriteFile(filepath.Join(w.source, "_index.yaml"), []byte(`namespace: host.deps
entries:
  - name: app
    kind: ns.dependency
    component: acme/app
    version: "<2.0.0"
`), 0o600))
	ctx := setupLoaderContext(t)
	requests := w.roots(ctx, t)
	require.Equal(t, []dependencyRequest{
		{Org: "acme", Module: "app"},
		{Org: "acme", Module: "app", Constraint: "<2.0.0"},
	}, requests, "explicit update releases only the implicit application's version constraint")
	requests, err := loadWorkspaceRoots(ctx, bootapi.GetLoader(ctx), w.source, w.configuredLock(t), payload.GetTranscoder(ctx), zap.NewNop(), w.cfg)
	require.NoError(t, err)
	require.Equal(t, "1.0.0", requests[0].Constraint, "startup keeps the locked application version")
	require.Equal(t, "<2.0.0", requests[1].Constraint)
}

func TestWorkspaceRootDiscoveryEnabledRequiresSource(t *testing.T) {
	w := newHostDependencyWorkspace(t, true)
	ctx := setupLoaderContext(t)
	for _, path := range []string{"", filepath.Join(w.root, "missing")} {
		_, err := loadUpdateRoots(ctx, bootapi.GetLoader(ctx), path, w.configuredLock(t), payload.GetTranscoder(ctx), zap.NewNop(), w.cfg)
		require.Error(t, err)
	}
}

func TestHostAndApplicationDependenciesUseOneConstraintGraph(t *testing.T) {
	for _, tc := range []struct {
		name       string
		constraint string
		conflict   bool
	}{
		{name: "compatible-movement", constraint: ">=1.2.0 <2.0.0"},
		{name: "conflict", constraint: ">=2.0.0", conflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newHostDependencyWorkspace(t, true)
			for _, item := range []struct{ path, constraint string }{
				{filepath.Join(w.app, "src", "_index.yaml"), "<2.0.0"},
				{filepath.Join(w.guide, "src", "_index.yaml"), tc.constraint},
			} {
				require.NoError(t, os.WriteFile(item.path, []byte("namespace: deps\nentries:\n  - name: lib\n    kind: ns.dependency\n    component: acme/lib\n    version: \""+item.constraint+"\"\n"), 0o600))
			}
			locked := w.configuredLock(t)
			locked.SetModule(lock.Module{Name: "acme/lib", Version: "1.0.0", Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
			require.NoError(t, locked.Write())
			before, err := os.ReadFile(w.lockPath)
			require.NoError(t, err)
			ctx := setupLoaderContext(t)
			provider := runManifestProvider{manifests: map[string]hub.ModuleManifest{}, versions: map[string][]hub.VersionInfo{
				"acme/lib": {{Version: "2.0.0"}, {Version: "1.2.0"}, {Version: "1.0.0"}},
			}}
			for _, version := range []string{"1.0.0", "1.2.0", "2.0.0"} {
				provider.manifests["acme/lib@"+version] = hub.ModuleManifest{Org: "acme", Name: "lib", Version: version,
					Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
			}
			resolved, err := resolveUpdatedWorkspaceDependencies(ctx, provider, locked, w.lockPath, w.cfg, w.roots(ctx, t), nil)
			if tc.conflict {
				require.ErrorContains(t, err, "acme/lib")
			} else {
				require.NoError(t, err)
				require.Contains(t, resolved, hub.ResolvedModule{Org: "acme", Name: "lib", Version: "1.2.0",
					Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
			}
			after, readErr := os.ReadFile(w.lockPath)
			require.NoError(t, readErr)
			require.Equal(t, before, after, "solving alone, including a rejected conflict, does not rewrite the lock")
		})
	}
}

func TestRunRootedWorkspaceLoadsNewLocalGuideAcrossRestarts(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(map[bool]string{false: "replaced-app", true: "cached-published-app"}[cached], func(t *testing.T) {
			runRootedWorkspaceGuide(t, cached)
		})
	}
}

func runRootedWorkspaceGuide(t *testing.T, cached bool) {
	t.Helper()
	w := newHostDependencyWorkspace(t, true)
	t.Chdir(w.root)
	resetRuntimeFlagGlobals(t)
	setTestConfigFiles(t)
	require.NoError(t, os.WriteFile(filepath.Join(w.root, ".wippy.yaml"), []byte(`version: "1.0"
shutdown:
  timeout: 5s
workspace:
  options:
    include_source_dependencies: true
  replacements:
    acme/app: ./app
    local/guide: ./guide
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(w.app, "src", "_index.yaml"), []byte(`namespace: app
entries:
  - name: terminal
    kind: terminal.host
    lifecycle:
      auto_start: true
  - name: probe
    kind: process.lua
    method: main
    imports:
      answer: local.guide:answer
    source: |
      local answer = require("answer")
      return {main = function()
        assert(answer.value == 42, "local guide did not load")
      end}
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(w.guide, "src", "_index.yaml"), []byte(`namespace: local.guide
entries:
  - name: answer
    kind: library.lua
    source: |
      return {value = 42}
`), 0o600))
	if cached {
		require.NoError(t, os.WriteFile(filepath.Join(w.root, ".wippy.yaml"), []byte(`version: "1.0"
shutdown:
  timeout: 5s
workspace:
  options:
    include_source_dependencies: true
  replacements:
    local/guide: ./guide
`), 0o600))
		w.cfg = bootapi.NewConfig(bootapi.WithSection("workspace", map[string]any{
			"options.include_source_dependencies": true,
			"replacements.local/guide":            w.guide,
		}))
		path := filepath.Join(w.root, w.configuredLock(t).GetVendorPath(), "acme", "app-1.0.0.wapp")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		data := writeSealedPack(t, path, wapp.Metadata{"name": "app", "namespace": "app", "version": "1.0.0"},
			wapp.Entry{ID: wapp.NewID("app", "terminal"), Kind: "terminal.host", Data: map[string]any{
				"lifecycle": map[string]any{"auto_start": true},
			}},
			wapp.Entry{ID: wapp.NewID("app", "probe"), Kind: "process.lua", Data: map[string]any{
				"method": "main", "imports": map[string]any{"answer": "local.guide:answer"},
				"source": `local answer = require("answer")
return {main = function()
  assert(answer.value == 42, "local guide did not load")
end}`,
			}})
		sum := sha256.Sum256(data)
		locked := w.configuredLock(t)
		locked.SetModule(lock.Module{Name: "acme/app", Version: "1.0.0", Root: true, Hash: hex.EncodeToString(sum[:])})
		require.NoError(t, locked.Write())
	}
	prevSilent := silentLogs
	t.Cleanup(func() { silentLogs = prevSilent })
	for range 2 {
		cmd := &cobra.Command{}
		cmd.Flags().String("exec", "", "")
		cmd.Flags().String("host", "", "")
		cmd.Flags().String("registry", "http://127.0.0.1:1", "")
		require.NoError(t, cmd.ParseFlags([]string{"--exec", "app:probe"}))
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		cmd.SetContext(ctx)
		err := runWithUseCase(cmd, cmd.Flags().Args(), defaultUseCase)
		cancel()
		require.NoError(t, err)
	}
}

func (w *hostDependencyWorkspace) writeRuntimeConfig(t *testing.T, enabled bool) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(w.root, ".wippy.yaml"), []byte(fmt.Sprintf(`version: "1.0"
workspace:
  options:
    include_source_dependencies: %t
  replacements:
    acme/app: ./app
    local/guide: ./guide
`, enabled)), 0o600))
}

func hostWorkspaceUpdateCommand(t *testing.T, registryURL string) *cobra.Command {
	t.Helper()
	cmd := runtimeConfigCommand(t, nil, nil)
	cmd.Flags().String("lock-file", "wippy.lock", "")
	cmd.Flags().String("src-dir", "./src", "")
	cmd.Flags().String("modules-dir", ".wippy", "")
	cmd.Flags().String("registry", registryURL, "")
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	cmd.SetContext(ctx)
	return cmd
}

func TestUpdateCommandHostDependencyOptIn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		enabled bool
	}{
		{name: "default-full"},
		{name: "default-local-target", args: []string{"local/guide"}},
		{name: "enabled-full", enabled: true},
		{name: "enabled-local-target", enabled: true, args: []string{"local/guide"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newHostDependencyWorkspace(t, tc.enabled)
			t.Chdir(w.root)
			resetRuntimeFlagGlobals(t)
			setTestConfigFiles(t)
			w.writeRuntimeConfig(t, tc.enabled)
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				http.Error(rw, "unexpected Hub request", http.StatusBadRequest)
			}))
			defer server.Close()
			require.NoError(t, runUpdate(hostWorkspaceUpdateCommand(t, server.URL), tc.args))
			selected := w.configuredLock(t)
			guide, found := selected.GetModule("local/guide")
			require.Equal(t, tc.enabled, found)
			if found {
				require.Equal(t, "0.0.0", guide.Version)
			}
			require.Equal(t, []string{"acme/app"}, selected.GetRootModules())
			app, found := selected.GetModule("acme/app")
			require.True(t, found)
			require.Equal(t, "1.0.0", app.Version)
			require.Zero(t, calls.Load())
			if !tc.enabled {
				return
			}
			w.writeHost(t, false)
			require.NoError(t, runUpdate(hostWorkspaceUpdateCommand(t, server.URL), tc.args))
			_, found = w.configuredLock(t).GetModule("local/guide")
			require.False(t, found)
			_, err := os.Stat(filepath.Join(w.guide, "src", "_index.yaml"))
			require.NoError(t, err, "pruning must not delete the local checkout")
		})
	}
}

func TestUpdateCommandMissingLocalSourceLeavesLockUnchanged(t *testing.T) {
	w := newHostDependencyWorkspace(t, true)
	t.Chdir(w.root)
	resetRuntimeFlagGlobals(t)
	setTestConfigFiles(t)
	w.writeRuntimeConfig(t, true)
	require.NoError(t, os.Rename(w.guide, w.guide+".hidden"))
	before, err := os.ReadFile(w.lockPath)
	require.NoError(t, err)
	err = runUpdate(hostWorkspaceUpdateCommand(t, "http://127.0.0.1:1"), nil)
	require.ErrorContains(t, err, "local/guide")
	after, err := os.ReadFile(w.lockPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestUpdateCommandDisablingHostDiscoveryPrunesHostOnlyDependency(t *testing.T) {
	w := newHostDependencyWorkspace(t, true)
	t.Chdir(w.root)
	resetRuntimeFlagGlobals(t)
	setTestConfigFiles(t)
	w.writeRuntimeConfig(t, true)
	require.NoError(t, runUpdate(hostWorkspaceUpdateCommand(t, "http://127.0.0.1:1"), nil))
	_, selected := w.configuredLock(t).GetModule("local/guide")
	require.True(t, selected)
	w.writeRuntimeConfig(t, false)
	require.NoError(t, runUpdate(hostWorkspaceUpdateCommand(t, "http://127.0.0.1:1"), nil))
	_, selected = w.configuredLock(t).GetModule("local/guide")
	require.False(t, selected, "explicit update must return to the application-only graph")
	require.Equal(t, []string{"acme/app"}, w.configuredLock(t).GetRootModules())
}
