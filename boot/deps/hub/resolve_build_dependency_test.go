// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

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
	for _, module := range result.Modules {
		roles[module.Org+"/"+module.Name] = module.BuildOnly
	}
	require.Equal(t, map[string]bool{
		"acme/frontend":      true,
		"acme/frontend-only": true,
		"acme/runtime":       false,
		"acme/shared":        false,
	}, roles)
}
