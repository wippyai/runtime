// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
)

func paramRoot(id, component, version string, params map[string]any) regapi.Entry {
	data := map[string]any{"component": component, "version": version}
	if len(params) > 0 {
		list := make([]map[string]any, 0, len(params))
		for name, value := range params {
			list = append(list, map[string]any{"name": name, "value": value})
		}
		data["parameters"] = list
	}
	return regapi.Entry{ID: regapi.ParseID(id), Kind: regapi.NamespaceDependency, Data: payload.New(data)}
}

// A committed reference whose controller's parameters drifted on disk must not
// wedge reconciliation of an unchanged baseline.
func TestReconcile_ParameterDriftOnControllerStillBoots(t *testing.T) {
	ctx := newTestContext()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: &fakeHub{}, Logger: zap.NewNop(), VendorDir: t.TempDir()})
	require.NoError(t, err)

	// Recorded at fold time with equal parameters...
	root := paramRoot("app.deps:a", "acme/a", "v1.0.0", map[string]any{"db": "app:db"})
	reference := paramRoot("acme.pkg:__dependency.acme.a", "acme/a", ">=1.0.0", map[string]any{"db": "app:db"})
	moduleEntry := hardeningModuleEntry("acme.a:entry", "acme/a", "v1.0.0")
	recordedState := regapi.State{root, reference, moduleEntry}

	transcoder := payload.GetTranscoder(ctx)
	baseline, err := handler.deploymentBaselineDigest(ctx, recordedState, transcoder)
	require.NoError(t, err)
	resolution := hardeningReferencedResolution([]regapi.Entry{root}, []regapi.Entry{reference})
	resolution.BaselineDigest = baseline
	resolution = resolution.Canonical()

	// ...then the controller's parameters change on disk.
	driftedRoot := paramRoot("app.deps:a", "acme/a", "v1.0.0", map[string]any{"db": "other:db"})
	driftedState := regapi.State{driftedRoot, reference, moduleEntry}

	result, err := handler.ReconcileResolution(ctx, driftedState, driftedState, resolution)
	require.NoError(t, err, "parameter drift must not wedge reconciliation")
	require.NotNil(t, result.Resolution)
}
