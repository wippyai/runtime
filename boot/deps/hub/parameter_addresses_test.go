// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build/stages"
)

func TestAuthoredBindingsOverrideArtifactByRequirementAddress(t *testing.T) {
	for _, test := range []struct {
		name, authored, artifact string
		sibling                  bool
	}{
		{"qualified_authored", "company.accounts:access", "access", false},
		{"qualified_artifact", "access", "company.accounts:access", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := newTestContext()
			dependency := regapi.Entry{ID: regapi.NewID("deployment.dependencies", "accounts"), Kind: regapi.NamespaceDependency,
				Data: payload.New(DependencyDefinition{Component: "acme/auth", Version: "2.0.0",
					Parameters: []Parameter{{Name: test.artifact, Value: false}, {Name: "introduced", Value: 42}}})}
			entries := []regapi.Entry{dependency,
				ownedEntry(regapi.Entry{ID: regapi.NewID("company.accounts", "definition"), Kind: regapi.NamespaceDefinition}, "acme/auth"),
				ownedEntry(regapi.Entry{ID: regapi.NewID("company.accounts", "target"), Kind: regapi.EntryKind, Meta: map[string]any{}}, "acme/auth"),
			}
			for _, namespace := range []string{"company.accounts", "company.accounts.team"} {
				path := ".meta.primary"
				if namespace == "company.accounts.team" {
					path = ".meta.sibling"
				}
				entries = append(entries, ownedEntry(regapi.Entry{ID: regapi.NewID(namespace, "access"), Kind: regapi.NamespaceRequirement,
					Data: payload.New(map[string]any{"default": false, "targets": []any{
						map[string]any{"entry": "company.accounts:target", "path": path},
					}})}, "acme/auth"))
			}
			entries = append(entries, ownedEntry(regapi.Entry{ID: regapi.NewID("company.accounts", "introduced"), Kind: regapi.NamespaceRequirement,
				Data: payload.New(map[string]any{"targets": []any{map[string]any{"entry": "target", "path": ".meta.introduced"}}})}, "acme/auth"))
			authored := dependency
			authored.Data = payload.New(DependencyDefinition{Component: "acme/auth", Version: "2.0.0",
				Parameters: []Parameter{{Name: test.authored, Value: true}}})
			changes := regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: authored}}
			linked, err := authoredDependencyEntries(ctx, entries, changes, payload.GetTranscoder(ctx))
			require.NoError(t, err)
			require.NoError(t, stages.Link(stages.WithDependencies(linked), stages.WithStrictRequirements()).Execute(ctx, &entries))
			for _, entry := range entries {
				if entry.ID == regapi.NewID("company.accounts", "target") {
					require.Equal(t, true, entry.Meta["primary"])
					require.Equal(t, test.sibling, entry.Meta["sibling"])
					require.EqualValues(t, 42, entry.Meta["introduced"])
				}
			}
			require.Equal(t, authored.Data.Data(), changes[0].Entry.Data.Data(), "authored stored parameters remain unchanged")
		})
	}
}
