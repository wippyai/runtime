package code

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
)

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
