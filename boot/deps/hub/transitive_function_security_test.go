// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	registryimpl "github.com/wippyai/runtime/system/registry"
	"github.com/wippyai/runtime/system/registry/expansion"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestRuntimeParentUpdatePublishesTransitiveFunctionSecurity(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) { testTransitiveFunctionSecurityHistory(t, engine) })
	}
}

func testTransitiveFunctionSecurityHistory(t *testing.T, engine string) {
	t.Helper()
	newContext := func() context.Context {
		return boot.WithConfig(newTestContext(), boot.NewConfig(boot.WithSection("override", map[string]any{
			"acme.child:settings:value": "host override",
		})))
	}
	ctx := regapi.WithDependencyAccess(newContext(), regapi.DependencyAccessOnline)
	directory := t.TempDir()
	vendor := filepath.Join(directory, "vendor")
	lockPath := filepath.Join(directory, "wippy.lock")
	parentID := regapi.NewID("host.modules", "parent")
	childID := regapi.NewID("host.modules", "child")
	functionID := regapi.NewID("acme.child", "handler")
	policyID := regapi.NewID("acme.child", "private_policy")
	const functionSource = "return {handler=function() return true end}"
	type selection struct{ name, version string }
	artifacts := make(map[selection][]byte)
	digests := make(map[selection]string)
	for _, release := range []string{"1.0.0", "2.0.0"} {
		function := wapp.Entry{ID: wapp.NewID(functionID.NS, functionID.Name), Kind: "function.lua",
			Data: map[string]any{"source": functionSource, "method": "handler"}}
		if release == "2.0.0" {
			function.Data.(map[string]any)["security"] = map[string]any{"policies": []string{policyID.String()}}
		}
		child := []wapp.Entry{
			{ID: wapp.NewID("acme.child", "definition"), Kind: regapi.NamespaceDefinition}, function,
			{ID: wapp.NewID("acme.child", "reader"), Kind: "library.lua", Data: map[string]any{"source": "return {version=\"" + release + "\"}"}},
			{ID: wapp.NewID("acme.child", "settings"), Kind: "registry.entry", Data: map[string]any{"value": release}},
		}
		if release == "2.0.0" {
			child = append(child, wapp.Entry{ID: wapp.NewID(policyID.NS, policyID.Name), Kind: "security.policy",
				Data: map[string]any{"policy": map[string]any{"effect": "allow", "actions": []string{"contract.get"}, "resources": []string{"acme.child:contract"}}}})
		}
		artifacts[selection{"child", release}] = buildWappBytes(t, child)
		artifacts[selection{"parent", release}] = buildWappBytes(t, []wapp.Entry{
			{ID: wapp.NewID("acme.parent", "definition"), Kind: regapi.NamespaceDefinition},
			{ID: wapp.NewID("acme.parent", "child"), Kind: regapi.NamespaceDependency,
				Data: rootClosureDependency{"acme/child", release}},
		})
	}
	for selected, artifact := range artifacts {
		digests[selected] = fmt.Sprintf("sha256:%x", sha256.Sum256(artifact))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(vendor, "acme"), 0o700))
	for _, name := range []string{"parent", "child"} {
		require.NoError(t, os.WriteFile(filepath.Join(vendor, "acme", name+"-1.0.0.wapp"), artifacts[selection{name, "1.0.0"}], 0o600))
	}
	require.NoError(t, os.WriteFile(lockPath, []byte(fmt.Sprintf(`directories:
  modules: vendor
modules:
  - name: acme/parent
    version: 1.0.0
    hash: %s
  - name: acme/child
    version: 1.0.0
    hash: %s
`, digests[selection{"parent", "1.0.0"}], digests[selection{"child", "1.0.0"}])), 0o600))
	var calls atomic.Int64
	client := &fakeHub{
		listVersions: func(context.Context, string, string) ([]VersionInfo, error) {
			calls.Add(1)
			return []VersionInfo{{Version: "2.0.0"}, {Version: "1.0.0"}}, nil
		},
		getManifest: func(_ context.Context, org, module, release string) (*ModuleManifest, error) {
			calls.Add(1)
			selected := selection{module, release}
			artifact, ok := artifacts[selected]
			if !ok {
				return nil, fmt.Errorf("unexpected artifact %s/%s@%s", org, module, release)
			}
			manifest := &ModuleManifest{Org: org, Name: module, Version: release, VersionID: release,
				Digest: digests[selected], SizeBytes: uint64(len(artifact)), URL: "memory://" + module + "@" + release}
			if module == "parent" {
				manifest.Dependencies = []ManifestDep{{Org: "acme", Name: "child", Version: release, Constraint: release,
					Digest: digests[selection{"child", release}]}}
			}
			return manifest, nil
		},
		downloadFile: func(_ context.Context, url, destination string) error {
			calls.Add(1)
			for selected, artifact := range artifacts {
				if url == "memory://"+selected.name+"@"+selected.version {
					require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0o700))
					return os.WriteFile(destination, artifact, 0o600)
				}
			}
			return fmt.Errorf("unexpected download %s", url)
		},
	}
	openHistory := deploymentRestartHistory(t, engine)
	history, closeHistory := openHistory()
	t.Cleanup(func() { closeHistory() })
	resolver := topology.NewResolver()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: client, Logger: zap.NewNop(), Resolver: resolver,
		LockPath: lockPath, VendorDir: vendor})
	require.NoError(t, err)
	t.Cleanup(handler.manifestCache.Close)
	require.NoError(t, handler.PrepareRestore(ctx, history))
	reg := registryimpl.NewRegistry(history, &bootRecordingRunner{}, topology.NewStateBuilder(zap.NewNop(), resolver), resolver,
		zap.NewNop(), registryimpl.WithKindDirective(regapi.NamespaceDependency, expansion.NewDependencyDirective(handler.Expand).
			WithChangesExpansion(handler.ExpandChanges).WithResolutionTransition(handler.ReconcileResolution)))
	ctx = regapi.WithRegistry(ctx, reg)
	parent := regapi.Entry{ID: parentID, Kind: regapi.NamespaceDependency,
		Data: payload.New(rootClosureDependency{"acme/parent", "1.0.0"})}
	child := regapi.Entry{ID: childID, Kind: regapi.NamespaceDependency,
		Data: payload.New(rootClosureDependency{"acme/child", "*"})}
	baseline := regapi.State{parent, child}
	for _, name := range []string{"parent", "child"} {
		entries, plan, err := handler.loadModuleEntries(ctx, []ResolvedModule{{Org: "acme", Name: name, Version: "1.0.0",
			Digest: digests[selection{name, "1.0.0"}]}}, baseline, payload.GetTranscoder(ctx))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, plan.cleanup()) })
		baseline = append(baseline, entries...)
	}
	authored := regapi.Entry{ID: regapi.NewID("host", "custom"), Kind: "registry.entry", Data: payload.New("retained")}
	baseline = append(baseline, authored)
	initial, err := history.Head()
	require.NoError(t, err)
	require.NoError(t, reg.LoadState(ctx, baseline, initial))
	parent.Data = payload.New(rootClosureDependency{"acme/parent", "2.0.0"})
	head, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: parent}})
	require.NoError(t, err)
	check := func(reg regapi.Registry, release string) {
		t.Helper()
		function, err := reg.GetEntry(functionID)
		require.NoError(t, err)
		var functionData struct {
			Source   string `json:"source"`
			Security struct {
				Policies []string `json:"policies"`
			} `json:"security"`
		}
		require.NoError(t, payload.GetTranscoder(ctx).Unmarshal(function.Data, &functionData))
		require.Equal(t, functionSource, functionData.Source)
		_, err = reg.GetEntry(policyID)
		if release == "2.0.0" {
			require.Equal(t, []string{policyID.String()}, functionData.Security.Policies,
				"a transitive version update must publish new security on an existing function")
			require.NoError(t, err)
		} else {
			require.Empty(t, functionData.Security.Policies)
			var missing apierror.Error
			require.ErrorAs(t, err, &missing, "rollback must remove the private policy introduced by the update")
			require.Equal(t, apierror.NotFound, missing.Kind())
		}
		reader, err := reg.GetEntry(regapi.NewID("acme.child", "reader"))
		require.NoError(t, err)
		var data map[string]any
		require.NoError(t, payload.GetTranscoder(ctx).Unmarshal(reader.Data, &data))
		require.Equal(t, "return {version=\""+release+"\"}", data["source"])
		settings, err := reg.GetEntry(regapi.NewID("acme.child", "settings"))
		require.NoError(t, err)
		require.NoError(t, payload.GetTranscoder(ctx).Unmarshal(settings.Data, &data))
		require.Equal(t, "host override", data["value"])
		retained, err := reg.GetEntry(authored.ID)
		require.NoError(t, err)
		require.True(t, entriesEqual(authored, retained))
		require.Equal(t, release, snapshotModuleVersion(t, reg, "acme/child"))
	}
	check(reg, "2.0.0")
	saved := reg.Snapshot().Registry.Resolution
	require.NoError(t, reg.ApplyVersion(ctx, initial))
	check(reg, "1.0.0")
	require.NoError(t, reg.ApplyVersion(ctx, head))
	check(reg, "2.0.0")
	require.Equal(t, saved, reg.Snapshot().Registry.Resolution)
	closeHistory()
	history, closeHistory = openHistory()
	// A real cold boot has a new application context and resolver. Reusing the
	// old context would keep its immutable registry binding and fake residency.
	offlineCtx := regapi.WithDependencyAccess(newContext(), regapi.DependencyAccessVerifiedOffline)
	restoredResolver := topology.NewResolver()
	beforeRestart := calls.Load()
	restoredHandler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: client, Logger: zap.NewNop(), Resolver: restoredResolver,
		LockPath: lockPath, VendorDir: vendor})
	require.NoError(t, err)
	t.Cleanup(restoredHandler.manifestCache.Close)
	require.NoError(t, restoredHandler.PrepareRestore(offlineCtx, history))
	restored := registryimpl.NewRegistry(history, &bootRecordingRunner{}, topology.NewStateBuilder(zap.NewNop(), restoredResolver), restoredResolver,
		zap.NewNop(), registryimpl.WithKindDirective(regapi.NamespaceDependency, expansion.NewDependencyDirective(restoredHandler.Expand).
			WithChangesExpansion(restoredHandler.ExpandChanges).WithResolutionTransition(restoredHandler.ReconcileResolution)))
	offlineCtx = regapi.WithRegistry(offlineCtx, restored)
	require.Same(t, restored, regapi.GetRegistry(offlineCtx))
	require.NoError(t, restored.LoadState(offlineCtx, baseline, head))
	require.Equal(t, beforeRestart, calls.Load(), "offline restore must not consult the Hub")
	check(restored, "2.0.0")
	require.Equal(t, saved, restored.Snapshot().Registry.Resolution, "restore must preserve exact selected versions and digests")
}
