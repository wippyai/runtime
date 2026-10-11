// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/lock"
	"go.uber.org/zap"
)

func TestExpandReplacementMissingRequirementCarriesDeclaration(t *testing.T) {
	ctx := newTestContext()
	root := t.TempDir()
	replacement := filepath.Join(root, "worker")
	require.NoError(t, os.MkdirAll(replacement, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(replacement, "wippy.yaml"), []byte("version: 1.0.0\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(replacement, "_index.json"), []byte(`{
	  "namespace": "example.worker",
	  "entries": [
	    {"name": "definition", "kind": "ns.definition", "meta": {"name": "example/worker", "version": "1.0.0"}, "data": {}},
	    {"name": "audience", "kind": "ns.requirement",
	     "meta": {"description": "Select the audience scope", "value_kind": "security.scope",
	              "options": ["host:read", "host:write"], "applicability": {"mode": "review"}},
	     "data": {"targets": [{"entry": "configuration", "path": ".meta.audience"}],
	              "options": {"allow_custom": false}}},
	    {"name": "configuration", "kind": "registry.entry", "meta": {}}
	  ]
	}`), 0o600))
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{getManifest: func(context.Context, string, string, string) (*ModuleManifest, error) {
			return nil, errors.New("replacement declarations must not come from the Hub")
		}},
		Logger: zap.NewNop(), VendorDir: filepath.Join(root, "vendor"), LockPath: filepath.Join(root, "wippy.lock"),
		WorkspaceReplacements: []lock.Replacement{{From: "example/worker", To: replacement}},
	})
	require.NoError(t, err)
	t.Cleanup(handler.manifestCache.Close)
	dependency := regapi.Entry{ID: regapi.NewID("host.dependencies", "worker"), Kind: regapi.NamespaceDependency,
		Data: payload.New(DependencyDefinition{Component: "example/worker", Version: "*"})}
	_, err = handler.ExpandChanges(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: dependency}}, nil)
	var diagnostic apierror.Error
	require.ErrorAs(t, err, &diagnostic)
	require.Equal(t, apierror.Invalid, diagnostic.Kind(), "%v", err)
	require.Equal(t, apierror.False, diagnostic.Retryable())
	value, ok := diagnostic.Details().Get("requirements")
	require.True(t, ok)
	rows := value.([]any)
	require.Len(t, rows, 1)
	row := rows[0].(map[string]any)
	require.Equal(t, "example.worker:audience", row["requirement"])
	require.Equal(t, []any{map[string]any{"entry": "configuration", "path": ".meta.audience"}}, row["targets"])
	require.Equal(t, map[string]any{
		"id": "example.worker:audience", "kind": "ns.requirement",
		"meta": map[string]any{"description": "Select the audience scope", "value_kind": "security.scope",
			"options": []any{"host:read", "host:write"}, "applicability": map[string]any{"mode": "review"}},
		"data": map[string]any{"targets": []any{map[string]any{"entry": "configuration", "path": ".meta.audience"}},
			"options": map[string]any{"allow_custom": false}},
	}, row["declaration"])

	dependency.Data = payload.New(DependencyDefinition{Component: "example/worker", Version: "*",
		Parameters: []Parameter{{Name: "audience", Value: "host:read"}}})
	result, err := handler.ExpandChanges(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: dependency}}, nil)
	require.NoError(t, err)
	for _, scoped := range result.Additional {
		if scoped.Operation.Entry.ID == regapi.NewID("example.worker", "configuration") {
			require.Equal(t, "host:read", scoped.Operation.Entry.Meta["audience"])
			return
		}
	}
	t.Fatal("expanded configuration is absent")
}
