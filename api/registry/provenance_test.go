// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProvenancedStateValidate(t *testing.T) {
	a := NewID("ns", "a")
	b := NewID("ns", "b")

	tests := []struct {
		want  error
		name  string
		state ProvenancedState
	}{
		{
			name: "complete host state",
			state: ProvenancedState{
				Entries: State{{ID: a}},
				Prov:    ProvMap{a: {}},
			},
		},
		{
			name:  "missing record",
			state: ProvenancedState{Entries: State{{ID: a}}, Prov: ProvMap{}},
			want:  ErrMissingProvenance,
		},
		{
			name:  "orphaned record",
			state: ProvenancedState{Prov: ProvMap{a: {}}},
			want:  ErrOrphanedProvenance,
		},
		{
			name: "conflicting module identity",
			state: ProvenancedState{
				Entries: State{{ID: a}, {ID: b}},
				Prov: ProvMap{
					a: {Module: "org/mod", Version: "1.0.0", Digest: "sha256:a"},
					b: {Module: "org/mod", Version: "2.0.0", Digest: "sha256:b"},
				},
			},
			want: ErrConflictingModuleProvenance,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.state.Validate()
			if tt.want == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.want)
		})
	}
}
