// SPDX-License-Identifier: MPL-2.0

package entries

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	depconfig "github.com/wippyai/runtime/boot/deps/config"
)

func TestPrepareRuntimeEntriesRejectsMissingRequiresWippy(t *testing.T) {
	entries := []regapi.Entry{{
		ID:   regapi.NewID("app.dependencies", "frontend"),
		Kind: regapi.NamespaceBuildDependency,
		Data: payload.New(map[string]any{"component": "acme/frontend", "version": "1.0.0"}),
	}}

	_, err := prepareRuntimeEntries(entries, &depconfig.ModuleConfig{}, "1.2.0")
	require.EqualError(t, err, "ns.build_dependency requires requires_wippy in wippy.yaml")
}

func TestPrepareRuntimeEntriesStripsCompatibleBuildDependencies(t *testing.T) {
	runtimeEntry := regapi.Entry{ID: regapi.NewID("app", "runtime"), Kind: "function.lua"}
	entries := []regapi.Entry{
		runtimeEntry,
		{
			ID:   regapi.NewID("app.dependencies", "frontend"),
			Kind: regapi.NamespaceBuildDependency,
			Data: payload.New(map[string]any{"component": "acme/frontend", "version": "1.0.0"}),
		},
	}

	prepared, err := prepareRuntimeEntries(entries, &depconfig.ModuleConfig{RequiresWippy: ">=1.2.0"}, "1.2.0")
	require.NoError(t, err)
	require.Equal(t, []regapi.Entry{runtimeEntry}, prepared)
}
