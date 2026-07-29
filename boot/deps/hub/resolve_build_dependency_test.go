// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveRuntimeWinsAcrossDifferentRootConstraints(t *testing.T) {
	provider := newFakeProvider()
	provider.addModule("acme", "shared", "1.0.0")
	provider.addModule("acme", "shared", "1.1.0")

	result, err := Resolve(context.Background(), provider, []DependencySpec{
		{Org: "acme", Name: "shared", Constraint: ">=1.0.0"},
		{Org: "acme", Name: "shared", Constraint: "1.1.0", BuildOnly: true},
	}, nil)
	require.NoError(t, err)
	require.Len(t, result.Modules, 1)
	require.Equal(t, "1.1.0", result.Modules[0].Version)
	require.False(t, result.Modules[0].BuildOnly)
	require.True(t, result.Modules[0].BuildDependency)
}

func TestResolveClassifiesBuildOnlyReachability(t *testing.T) {
	provider := newFakeProvider()
	provider.addModule("acme", "runtime", "1.0.0", ManifestDep{Org: "acme", Name: "shared", Version: "1.0.0"})
	provider.addModule("acme", "frontend", "1.0.0",
		ManifestDep{Org: "acme", Name: "shared", Version: "1.0.0"},
		ManifestDep{Org: "acme", Name: "frontend-only", Version: "1.0.0"},
	)
	provider.addModule("acme", "shared", "1.0.0")
	provider.addModule("acme", "frontend-only", "1.0.0")

	result, err := Resolve(context.Background(), provider, []DependencySpec{
		{Org: "acme", Name: "runtime", Constraint: "1.0.0"},
		{Org: "acme", Name: "frontend", Constraint: "1.0.0", BuildOnly: true},
	}, nil)
	require.NoError(t, err)

	roles := make(map[string]bool, len(result.Modules))
	buildDependencies := make(map[string]bool, len(result.Modules))
	for _, module := range result.Modules {
		name := module.Org + "/" + module.Name
		roles[name] = module.BuildOnly
		buildDependencies[name] = module.BuildDependency
	}
	require.Equal(t, map[string]bool{
		"acme/frontend":      true,
		"acme/frontend-only": true,
		"acme/runtime":       false,
		"acme/shared":        false,
	}, roles)
	require.Equal(t, map[string]bool{
		"acme/frontend":      true,
		"acme/frontend-only": true,
		"acme/runtime":       false,
		"acme/shared":        true,
	}, buildDependencies)
}
