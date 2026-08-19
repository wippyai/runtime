// SPDX-License-Identifier: MPL-2.0

package code

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"go.uber.org/zap"
)

func TestBuiltinNodeContentHashMatchesLintSeed(t *testing.T) {
	initial := &api.ModuleDef{
		Name:  "initial-module",
		Types: func() *io.Manifest { return io.NewManifest("initial-module") },
	}
	late := &api.ModuleDef{
		Name:  "late-module",
		Types: func() *io.Manifest { return io.NewManifest("late-module") },
	}

	cm, err := NewCodeManager(zap.NewNop(), &testEventBus{}, Config{Modules: []*api.ModuleDef{initial}})
	require.NoError(t, err)

	initialNode, err := cm.memGraph.GetNode(registry.NewID("", initial.Name))
	require.NoError(t, err)
	lateNode := Node{
		ID:       registry.NewID("", late.Name),
		Kind:     api.ModuleKind,
		Module:   late,
		Manifest: late.Types(),
	}
	require.NoError(t, cm.AddNode(nil, lateNode, nil))
	cm.AddBuiltinType(late)

	for _, node := range []*Node{initialNode, mustGetNode(t, cm, lateNode.ID)} {
		if got := nodeContentHash(node); got != cache.SourceHash("", "") {
			t.Fatalf("builtin %s content hash = %q, want canonical lint seed %q", node.ID, got, cache.SourceHash("", ""))
		}
	}
}

func TestContentAddressedCacheSurvivesGraphMutation(t *testing.T) {
	typeCfg := DefaultTypeCheckConfig()
	typeCfg.Enabled = true
	typeCfg.Strict = true
	cm, err := NewCodeManager(zap.NewNop(), &testEventBus{}, Config{
		TypeCheck: typeCfg,
		Cache: cache.Config{
			Dir:              t.TempDir(),
			Enabled:          true,
			CompileEnabled:   true,
			TypecheckEnabled: true,
		},
	})
	require.NoError(t, err)

	id := registry.NewID("test.cache", "entry")
	ctx := context.Background()
	require.NoError(t, cm.AddNode(ctx, Node{ID: id, Kind: api.Function, Source: `return "old"`, Method: "main"}, nil))
	compileFP, _, err := cm.compileFingerprint(id)
	require.NoError(t, err)
	typecheckFP, _, err := cm.typecheckFingerprint(id)
	require.NoError(t, err)
	_, err = cm.Compile(id, nil)
	require.NoError(t, err)

	for _, key := range []string{cm.compileCacheKey(compileFP), cm.typecheckCacheKey(typecheckFP)} {
		_, ok, getErr := cm.cacheStore.Get(key)
		require.NoError(t, getErr)
		require.True(t, ok)
	}

	require.NoError(t, cm.UpdateNode(ctx, Node{ID: id, Kind: api.Function, Source: `return "new"`, Method: "main"}, nil))
	require.NoError(t, cm.DeleteNode(ctx, id))
	for _, key := range []string{cm.compileCacheKey(compileFP), cm.typecheckCacheKey(typecheckFP)} {
		_, ok, getErr := cm.cacheStore.Get(key)
		require.NoError(t, getErr)
		require.True(t, ok)
	}
}

func TestRuntimeFingerprintsMatchLintBuiltinSeeds(t *testing.T) {
	initial := &api.ModuleDef{
		Name:  "initial-module",
		Types: func() *io.Manifest { return io.NewManifest("initial-module") },
	}
	late := &api.ModuleDef{
		Name:  "late-module",
		Types: func() *io.Manifest { return io.NewManifest("late-module") },
	}
	typeCfg := TypeCheckConfig{Enabled: true, Strict: true}
	cm, err := NewCodeManager(zap.NewNop(), &testEventBus{}, Config{
		Modules:   []*api.ModuleDef{initial},
		TypeCheck: typeCfg,
	})
	require.NoError(t, err)

	initialID := registry.NewID("", initial.Name)
	lateID := registry.NewID("", late.Name)
	userID := registry.NewID("app", "main")
	require.NoError(t, cm.AddNode(nil, Node{
		ID:       lateID,
		Kind:     api.ModuleKind,
		Module:   late,
		Manifest: late.Types(),
	}, nil))
	cm.AddBuiltinType(late)
	require.NoError(t, cm.AddNode(nil, Node{
		ID:     userID,
		Kind:   api.Function,
		Source: "return initial + late",
		Method: "main",
	}, []Import{
		{ID: initialID, Alias: "initial"},
		{ID: lateID, Alias: "late"},
	}))

	compileFP, _, err := cm.compileFingerprint(userID)
	require.NoError(t, err)
	initialCompileFP := CompileFingerprint(initialID.String(), api.ModuleKind, cache.SourceHash("", ""), "", nil)
	lateCompileFP := CompileFingerprint(lateID.String(), api.ModuleKind, cache.SourceHash("", ""), "", nil)
	wantCompileFP := CompileFingerprint(userID.String(), api.Function, cache.SourceHash("return initial + late", "main"), "main", []cache.DepFingerprint{
		{Alias: "initial", ID: initialID.String(), Fingerprint: initialCompileFP},
		{Alias: "late", ID: lateID.String(), Fingerprint: lateCompileFP},
	})
	if compileFP != wantCompileFP {
		t.Fatalf("runtime compile fingerprint = %q, lint formula = %q", compileFP, wantCompileFP)
	}

	typeHash := TypecheckConfigHash(typeCfg)
	builtinHash := BuiltinManifestHash(map[string]*io.Manifest{
		initial.Name: initial.Types(),
		late.Name:    late.Types(),
	})
	typeFP, _, err := cm.typecheckFingerprint(userID)
	require.NoError(t, err)
	initialTypeFP := TypecheckFingerprint(initialID.String(), api.ModuleKind, cache.SourceHash("", ""), "", typeHash, builtinHash, nil)
	lateTypeFP := TypecheckFingerprint(lateID.String(), api.ModuleKind, cache.SourceHash("", ""), "", typeHash, builtinHash, nil)
	wantTypeFP := TypecheckFingerprint(userID.String(), api.Function, cache.SourceHash("return initial + late", "main"), "main", typeHash, builtinHash, []cache.DepFingerprint{
		{Alias: "initial", ID: initialID.String(), Fingerprint: initialTypeFP},
		{Alias: "late", ID: lateID.String(), Fingerprint: lateTypeFP},
	})
	if typeFP != wantTypeFP {
		t.Fatalf("runtime typecheck fingerprint = %q, lint formula = %q", typeFP, wantTypeFP)
	}
}

func mustGetNode(t *testing.T, cm *Manager, id registry.ID) *Node {
	t.Helper()
	node, err := cm.memGraph.GetNode(id)
	require.NoError(t, err)
	return node
}

func TestCompileFingerprintCascades(t *testing.T) {
	cm, libNode, appID := setupFingerprintGraph(t)

	fp1, _, err := cm.compileFingerprint(appID)
	require.NoError(t, err)

	libNode.Source = "return 2"

	fp2, _, err := cm.compileFingerprint(appID)
	require.NoError(t, err)

	assert.NotEqual(t, fp1, fp2)
}

func TestTypecheckFingerprintCascades(t *testing.T) {
	cm, libNode, appID := setupFingerprintGraph(t)

	fp1, _, err := cm.typecheckFingerprint(appID)
	require.NoError(t, err)

	libNode.Source = "return 2"

	fp2, _, err := cm.typecheckFingerprint(appID)
	require.NoError(t, err)

	assert.NotEqual(t, fp1, fp2)
}

func setupFingerprintGraph(t *testing.T) (*Manager, *Node, registry.ID) {
	t.Helper()

	cm := &Manager{
		memGraph:    NewMemoryGraph(),
		typeCfgHash: "typecfg",
		builtinHash: "builtin",
	}

	libID := registry.NewID("lib", "util")
	appID := registry.NewID("app", "main")

	libNode := &Node{
		ID:     libID,
		Kind:   api.Library,
		Source: "return 1",
	}
	appNode := &Node{
		ID:     appID,
		Kind:   api.Function,
		Source: "return util()",
		Method: "main",
	}

	require.NoError(t, cm.memGraph.AddNode(libNode))
	require.NoError(t, cm.memGraph.AddNode(appNode))
	require.NoError(t, cm.memGraph.AddDependency(appID, libID, "util"))

	return cm, libNode, appID
}
