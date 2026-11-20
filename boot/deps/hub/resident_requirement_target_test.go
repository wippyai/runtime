// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build/stages"
	"github.com/wippyai/runtime/boot/deps/lock"
	regtop "github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func residentRequirementTargetHandler(t *testing.T) *DependencyHandler {
	t.Helper()
	vendorDir := filepath.Join(t.TempDir(), "vendor")
	writeWapp(t, filepath.Join(vendorDir, "acme", "component-v1.0.0.wapp"), []wapp.Entry{
		{ID: wapp.NewID("acme.component", "validator"), Kind: "function.lua",
			Data: map[string]any{"source": "return {}", "imports": map[string]any{"base": "acme.component:base"}}},
		{ID: wapp.NewID("acme.component", "base"), Kind: "function.lua", Data: map[string]any{"source": "return {}"}},
		{ID: wapp.NewID("acme.component", "unrelated"), Kind: "function.lua", Data: map[string]any{"source": "return {}"}},
	})
	writeWapp(t, filepath.Join(vendorDir, "acme", "workflows-v1.0.0.wapp"), []wapp.Entry{
		{ID: wapp.NewID("acme.workflows", "admission_import"), Kind: regapi.NamespaceRequirement,
			Data: map[string]any{
				"default": "acme.workflows:admission",
				"targets": []any{map[string]any{"entry": "acme.component:validator", "path": ".imports.workflow_admission"}},
			}},
		{ID: wapp.NewID("acme.workflows", "admission"), Kind: "function.lua", Data: map[string]any{"source": "return {}"}},
	})
	resolver := regtop.NewResolver()
	require.NoError(t, resolver.RegisterPattern(regapi.DependencyPattern{
		Path: "data.imports.*", Description: "Imported entries", AllowWildcard: true,
	}))
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
			return &ModuleManifest{Org: org, Name: module, Version: version}, nil
		}},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
		Resolver:  resolver,
	})
	require.NoError(t, err)
	t.Cleanup(handler.manifestCache.Close)
	handler.deployment = &regapi.Deployment{Modules: []regapi.ResolvedModule{
		{Name: "acme/component", Version: "v1.0.0"}, {Name: "acme/workflows", Version: "v1.0.0"},
	}}
	return handler
}

func residentComponentEntries(imports string) regapi.State {
	owner := regapi.EntryMetadata{Owner: "acme/component"}
	return regapi.State{
		{ID: regapi.NewID("acme.component", "validator"), Kind: "function.lua", Registry: owner,
			Data: payload.NewPayload(`{"source":"return {}","imports":`+imports+`}`, payload.JSON)},
		{ID: regapi.NewID("acme.component", "base"), Kind: "function.lua", Registry: owner,
			Data: payload.NewPayload(`{"source":"return {}"}`, payload.JSON)},
		// Normalization drift on an unrelated resident entry must stay untouched.
		{ID: regapi.NewID("acme.component", "unrelated"), Kind: "function.lua", Registry: owner,
			Data: payload.NewPayload(`{"source":"return {}","resident":true}`, payload.JSON)},
	}
}

func residentRoot(name, component string) regapi.Entry {
	return regapi.Entry{ID: regapi.NewID("app.deps", name), Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"`+component+`","version":"v1.0.0"}`, payload.JSON)}
}

func requireValidatorImports(t *testing.T, ops []regapi.ScopedOperation, want map[string]any) {
	t.Helper()
	var validator *regapi.Entry
	for i := range ops {
		op := ops[i].Operation
		if op.Entry.ID == regapi.NewID("acme.component", "unrelated") {
			t.Fatalf("unrelated resident entry changed: %v", op.Kind)
		}
		if op.Entry.ID == regapi.NewID("acme.component", "validator") {
			require.Equal(t, regapi.EntryUpdate, op.Kind)
			validator = &ops[i].Operation.Entry
		}
	}
	require.NotNil(t, validator, "resident requirement target must be updated")
	data, ok := validator.Data.Data().(map[string]any)
	if !ok {
		var decoded map[string]any
		require.NoError(t, payload.GetTranscoder(newTestContext()).Unmarshal(validator.Data, &decoded))
		data = decoded
	}
	require.Equal(t, want, data["imports"])
}

func TestHotInstallUpdatesResidentRequirementTarget(t *testing.T) {
	handler := residentRequirementTargetHandler(t)
	componentRoot := residentRoot("component", "acme/component")
	workflowsRoot := residentRoot("workflows", "acme/workflows")
	snapshot := append(regapi.State{componentRoot}, residentComponentEntries(`{"base":"acme.component:base"}`)...)

	result, err := handler.Expand(newTestContext(), regapi.Operation{Kind: regapi.EntryCreate, Entry: workflowsRoot}, snapshot)
	require.NoError(t, err)
	require.True(t, result.Applied)
	requireValidatorImports(t, result.Additional, map[string]any{
		"base": "acme.component:base", "workflow_admission": "acme.workflows:admission",
	})
}

func TestUninstallRemovesResidentRequirementTarget(t *testing.T) {
	handler := residentRequirementTargetHandler(t)
	componentRoot := residentRoot("component", "acme/component")
	workflowsRoot := residentRoot("workflows", "acme/workflows")
	owner := regapi.EntryMetadata{Owner: "acme/workflows"}
	snapshot := append(regapi.State{componentRoot, workflowsRoot,
		{ID: regapi.NewID("acme.workflows", "admission_import"), Kind: regapi.NamespaceRequirement, Registry: owner,
			Data: payload.NewPayload(`{"default":"acme.workflows:admission","targets":[{"entry":"acme.component:validator","path":".imports.workflow_admission"}]}`, payload.JSON)},
		{ID: regapi.NewID("acme.workflows", "admission"), Kind: "function.lua", Registry: owner,
			Data: payload.NewPayload(`{"source":"return {}"}`, payload.JSON)},
	}, residentComponentEntries(`{"base":"acme.component:base","workflow_admission":"acme.workflows:admission"}`)...)

	result, err := handler.Expand(newTestContext(), regapi.Operation{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: workflowsRoot.ID}}, snapshot)
	require.NoError(t, err)
	require.True(t, result.Applied)
	requireValidatorImports(t, result.Additional, map[string]any{"base": "acme.component:base"})
	deleted := map[regapi.ID]bool{}
	for _, op := range result.Additional {
		if op.Operation.Kind == regapi.EntryDelete {
			deleted[op.Operation.Entry.ID] = true
		}
	}
	require.True(t, deleted[regapi.NewID("acme.workflows", "admission")], "removed library must be deleted")
}

func TestResidentRequirementTargetFollowsInstallRestartUndoAndUninstall(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			root := t.TempDir()
			fixture := &deploymentRestartFixture{
				lockPath: filepath.Join(root, "wippy.lock"), vendorDir: filepath.Join(root, "vendor"),
				modules: []string{"app", "component"}, artifacts: map[string][]byte{}, digests: map[string]string{},
			}
			modules := map[string][]wapp.Entry{
				"app": {{ID: wapp.NewID("acme.app", "definition"), Kind: regapi.NamespaceDefinition}},
				"component": {
					{ID: wapp.NewID("acme.component", "validator"), Kind: "function.lua",
						Data: map[string]any{"source": "return {}", "imports": map[string]any{"base": "acme.component:base"}}},
					{ID: wapp.NewID("acme.component", "base"), Kind: "function.lua", Data: map[string]any{"source": "return {}"}},
				},
				"workflows": {
					{ID: wapp.NewID("acme.workflows", "admission_import"), Kind: regapi.NamespaceRequirement,
						Data: map[string]any{
							"default": "acme.workflows:admission",
							"targets": []any{map[string]any{"entry": "acme.component:validator", "path": ".imports.workflow_admission"}},
						}},
					{ID: wapp.NewID("acme.workflows", "admission"), Kind: "function.lua", Data: map[string]any{"source": "return {}"}},
				},
			}
			for module, entries := range modules {
				key := module + "@1.0.0"
				fixture.artifacts[key] = buildWappBytes(t, entries)
				sum := sha256.Sum256(fixture.artifacts[key])
				fixture.digests[key] = "sha256:" + hex.EncodeToString(sum[:])
			}
			l, err := lock.New(fixture.lockPath)
			require.NoError(t, err)
			l.SetDirectories(lock.Directories{Modules: "vendor"})
			require.NoError(t, os.MkdirAll(filepath.Join(fixture.vendorDir, "acme"), 0o755))
			for _, module := range fixture.modules {
				l.SetModule(lock.Module{Name: "acme/" + module, Version: "1.0.0", Hash: strings.TrimPrefix(fixture.digests[module+"@1.0.0"], "sha256:")})
				require.NoError(t, os.WriteFile(filepath.Join(fixture.vendorDir, "acme", module+"-1.0.0.wapp"), fixture.artifacts[module+"@1.0.0"], 0o600))
				for _, entry := range modules[module] {
					fixture.baseline = append(fixture.baseline, regapi.Entry{
						ID: regapi.NewID(entry.ID.Namespace, entry.ID.Name), Kind: entry.Kind, Data: payload.New(entry.Data),
						Registry: regapi.EntryMetadata{Owner: "acme/" + module},
					})
				}
			}
			require.NoError(t, l.Write())
			fixture.baseline = append(fixture.baseline, regapi.Entry{
				ID: regapi.NewID("host.deps", "app"), Kind: regapi.NamespaceDependency,
				Data: payload.New(DependencyDefinition{Component: "acme/app", Version: "*"}),
			})
			baseline := []regapi.Entry(fixture.baseline)
			require.NoError(t, stages.Link().Execute(newTestContext(), &baseline))
			fixture.baseline = baseline

			validatorID := regapi.NewID("acme.component", "validator")
			admissionID := regapi.NewID("acme.workflows", "admission")
			check := func(reg regapi.Registry, installed bool) {
				t.Helper()
				entry, err := reg.GetEntry(validatorID)
				require.NoError(t, err)
				imports := map[string]any{"base": "acme.component:base"}
				if installed {
					imports["workflow_admission"] = "acme.workflows:admission"
				}
				var data map[string]any
				require.NoError(t, payload.GetTranscoder(newTestContext()).Unmarshal(entry.Data, &data))
				require.Equal(t, imports, data["imports"])
				_, err = reg.GetEntry(admissionID)
				if installed {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			}

			openHistory := deploymentRestartHistory(t, engine)
			history, closeHistory := openHistory()
			ctx, live, _ := fixture.registry(t, history, false)
			initial, err := history.Head()
			require.NoError(t, err)
			require.NoError(t, live.LoadState(ctx, fixture.baseline, initial))
			check(live, false)

			workflowsRoot := regapi.Entry{ID: regapi.NewID("host.deps", "workflows"), Kind: regapi.NamespaceDependency,
				Data: payload.New(DependencyDefinition{Component: "acme/workflows", Version: "1.0.0"})}
			installed, err := live.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: workflowsRoot}})
			require.NoError(t, err)
			check(live, true)
			closeHistory()

			history, closeHistory = openHistory()
			defer closeHistory()
			ctx, restored, _ := fixture.registry(t, history, false)
			require.NoError(t, restored.LoadState(ctx, fixture.baseline, installed))
			check(restored, true)
			require.NoError(t, restored.ApplyVersion(ctx, initial))
			check(restored, false)
			require.NoError(t, restored.ApplyVersion(ctx, installed))
			check(restored, true)

			_, err = restored.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: workflowsRoot.ID}}})
			require.NoError(t, err)
			check(restored, false)
		})
	}
}
