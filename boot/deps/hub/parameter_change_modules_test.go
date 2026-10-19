// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
)

func TestChangedDependencyParameterModules(t *testing.T) {
	declaration := func(component, version string, params ...Parameter) regapi.State {
		return regapi.State{{
			ID: regapi.NewID("parent.deps", "child"), Kind: regapi.NamespaceDependency,
			Registry: regapi.EntryMetadata{Owner: "acme/parent"},
			Data:     payload.New(DependencyDefinition{Component: component, Version: version, Parameters: params}),
		}}
	}
	param := Parameter{Name: "setting", Value: "configured"}
	for _, tc := range []struct {
		name          string
		before, after regapi.State
		want          []string
	}{
		{name: "unchanged", before: declaration("acme/child", "1.0.0", param), after: declaration("acme/child", "1.0.0", param)},
		{name: "version only", before: declaration("acme/child", "1.0.0", param), after: declaration("acme/child", "2.0.0", param)},
		{name: "parameterless addition", after: declaration("acme/child", "1.0.0")},
		{name: "parameterless removal", before: declaration("acme/child", "1.0.0")},
		{name: "parameter addition", before: declaration("acme/child", "1.0.0"), after: declaration("acme/child", "1.0.0", param), want: []string{"acme/child"}},
		{name: "parameter removal", before: declaration("acme/child", "1.0.0", param), after: declaration("acme/child", "1.0.0"), want: []string{"acme/child"}},
		{name: "declaration addition", after: declaration("acme/child", "1.0.0", param), want: []string{"acme/child"}},
		{name: "declaration removal", before: declaration("acme/child", "1.0.0", param), want: []string{"acme/child"}},
		{name: "component replacement", before: declaration("acme/old", "1.0.0", param), after: declaration("acme/new", "1.0.0", param), want: []string{"acme/old", "acme/new"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newTestContext()
			changed, err := changedDependencyParameterModules(ctx, tc.before, tc.after, payload.GetTranscoder(ctx))
			require.NoError(t, err)
			require.ElementsMatch(t, tc.want, sortedSetKeys(changed))
		})
	}
}
