// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"testing"

	"github.com/stretchr/testify/require"
	regapi "github.com/wippyai/runtime/api/registry"
)

func TestMissingControllingDependencyVersionNamesEntry(t *testing.T) {
	root := desiredDependency{
		entry:      regapi.Entry{ID: regapi.NewID("app", "core")},
		definition: DependencyDefinition{Component: "acme/core"},
	}
	_, _, err := foldRootDependencyComponents([]desiredDependency{root}, nil, true)
	require.ErrorContains(t, err, "app:core")
	require.ErrorContains(t, err, "acme/core")
	require.ErrorContains(t, err, "version")
	root.definition.Version = "*"
	roots, references, err := foldRootDependencyComponents([]desiredDependency{root}, nil, true)
	require.NoError(t, err)
	require.Len(t, roots, 1)
	require.Empty(t, references)
}

func TestUnversionedFoldedReferenceStillAllowed(t *testing.T) {
	root := desiredDependency{entry: regapi.Entry{ID: regapi.NewID("app", "a")}, definition: DependencyDefinition{Component: "acme/core", Version: "1.0.0"}}
	reference := desiredDependency{entry: regapi.Entry{ID: regapi.NewID("app", "b")}, definition: DependencyDefinition{Component: "acme/core"}}
	roots, references, err := foldRootDependencyComponents([]desiredDependency{reference, root}, nil, true)
	require.NoError(t, err)
	require.Len(t, roots, 1)
	require.Equal(t, "1.0.0", roots[0].definition.Version)
	require.Len(t, references, 1)
	require.Equal(t, "*", references[0].definition.Version)
}
