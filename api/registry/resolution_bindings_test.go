// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func bindingResolution() *DependencyResolution {
	return (&DependencyResolution{
		InputDigest: "inputs", BaselineDigest: "baseline",
		Roots:      []DependencyRoot{{ID: "app:worker", Component: "acme/worker", Version: "1.0.0"}},
		References: []DependencyRoot{{ID: "app:alias", Component: "acme/worker", Version: "1.0.0"}},
		Modules:    []ResolvedModule{{Name: "acme/worker", Version: "1.0.0", Digest: "artifact"}},
	}).Canonical()
}

func TestDependencyBindingsAreDetachedAndDigestRelevant(t *testing.T) {
	legacy := bindingResolution()
	empty := legacy.Canonical()
	empty.Bindings = []DependencyBinding{}
	require.Equal(t, legacy.Digest, empty.Canonical().Digest, "unannotated graphs keep their exact historic digest")
	bound := legacy.Canonical()
	bound.Bindings = []DependencyBinding{
		{ID: "app:worker", Parameters: json.RawMessage(`[{"name":"flag","value":false},{"name":"count","value":42},{"name":"empty","value":null},{"name":"object","value":{"items":[true,1,"value"]}}]`)},
		{ID: "app:alias", Parameters: json.RawMessage(`[]`)},
	}
	canonical := bound.Canonical()
	require.True(t, canonical.Valid())
	require.NotEqual(t, legacy.Digest, canonical.Digest)
	require.Equal(t, "app:alias", canonical.Bindings[0].ID)
	bound.Bindings[0].Parameters[0] = '!'
	require.True(t, canonical.Valid(), "canonical bindings do not share backing bytes")
	encoded, err := json.Marshal(canonical)
	require.NoError(t, err)
	var restored DependencyResolution
	require.NoError(t, json.Unmarshal(encoded, &restored))
	require.Equal(t, canonical, restored.Canonical())
	require.True(t, restored.Valid())
	require.False(t, CanRebaseDependencyResolution(legacy, canonical), "binding metadata cannot rewrite an immutable old version")
	corrupted := canonical.Canonical()
	corrupted.Bindings[1].Parameters[0] = '!'
	require.False(t, corrupted.Valid(), "digest validation must detect modified parameters")
	require.Empty(t, corrupted.Canonical().Digest, "malformed JSON cannot acquire a content identity")
}

func TestDependencyBindingsRejectUnanchoredDuplicateAndMalformedRecords(t *testing.T) {
	for _, test := range []struct {
		name     string
		bindings []DependencyBinding
	}{
		{"unanchored", []DependencyBinding{{ID: "app:missing", Parameters: json.RawMessage(`[]`)}}},
		{"duplicate", []DependencyBinding{{ID: "app:worker", Parameters: json.RawMessage(`[]`)}, {ID: "app:worker", Parameters: json.RawMessage(`[]`)}}},
		{"null", []DependencyBinding{{ID: "app:worker", Parameters: json.RawMessage(`null`)}}},
		{"object", []DependencyBinding{{ID: "app:worker", Parameters: json.RawMessage(`{}`)}}},
		{"invalid-json", []DependencyBinding{{ID: "app:worker", Parameters: json.RawMessage(`[`)}}},
		{"missing-name", []DependencyBinding{{ID: "app:worker", Parameters: json.RawMessage(`[{"value":42}]`)}}},
		{"missing-value", []DependencyBinding{{ID: "app:worker", Parameters: json.RawMessage(`[{"name":"x"}]`)}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph := bindingResolution()
			graph.Bindings = test.bindings
			require.False(t, graph.Canonical().Valid())
		})
	}
}
