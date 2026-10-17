// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	registryimpl "github.com/wippyai/runtime/system/registry"
	"github.com/wippyai/runtime/system/registry/expansion"
	historysqlite "github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestRuntimeParentUpdatePublishesTransitiveFunctionSecurity(t *testing.T) {
	ctx := boot.WithConfig(newTestContext(), boot.NewConfig(boot.WithSection("override", map[string]any{
		"acme.child:settings:value": "host override",
	})))
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
	client := &fakeHub{
		listVersions: func(context.Context, string, string) ([]VersionInfo, error) {
			return []VersionInfo{{Version: "2.0.0"}, {Version: "1.0.0"}}, nil
		},
		getManifest: func(_ context.Context, org, module, release string) (*ModuleManifest, error) {
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
			for selected, artifact := range artifacts {
				if url == "memory://"+selected.name+"@"+selected.version {
					require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0o700))
					return os.WriteFile(destination, artifact, 0o600)
				}
			}
			return fmt.Errorf("unexpected download %s", url)
		},
	}
	history, err := historysqlite.NewSQLite(filepath.Join(directory, "history.db"), zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, history.Close()) })
	resolver := topology.NewResolver()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: client, Logger: zap.NewNop(), Resolver: resolver,
		LockPath: lockPath, VendorDir: vendor})
	require.NoError(t, err)
	t.Cleanup(handler.manifestCache.Close)
	reg := registryimpl.NewRegistry(history, &bootRecordingRunner{}, topology.NewStateBuilder(zap.NewNop(), resolver), resolver,
		zap.NewNop(), registryimpl.WithKindDirective(regapi.NamespaceDependency, expansion.NewDependencyDirective(handler.Expand).
			WithChangesExpansion(handler.ExpandChanges).WithResolutionTransition(handler.ReconcileResolution)))
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
	require.NoError(t, reg.LoadState(ctx, baseline, version.FromParent(nil, regapi.RootVersion)))
	parent.Data = payload.New(rootClosureDependency{"acme/parent", "2.0.0"})
	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: parent}})
	require.NoError(t, err)
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
	require.Equal(t, []string{policyID.String()}, functionData.Security.Policies,
		"a transitive version update must publish new security on an existing function")
	_, err = reg.GetEntry(policyID)
	require.NoError(t, err)
	reader, err := reg.GetEntry(regapi.NewID("acme.child", "reader"))
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, payload.GetTranscoder(ctx).Unmarshal(reader.Data, &data))
	require.Equal(t, "return {version=\"2.0.0\"}", data["source"])
	settings, err := reg.GetEntry(regapi.NewID("acme.child", "settings"))
	require.NoError(t, err)
	require.NoError(t, payload.GetTranscoder(ctx).Unmarshal(settings.Data, &data))
	require.Equal(t, "host override", data["value"])
	retained, err := reg.GetEntry(authored.ID)
	require.NoError(t, err)
	require.True(t, entriesEqual(authored, retained))
	require.Equal(t, "2.0.0", snapshotModuleVersion(t, reg, "acme/child"))
}
