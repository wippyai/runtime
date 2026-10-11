// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

func TestLinkUnresolvedRequirementsCarryAddressesAndTargets(t *testing.T) {
	ctx, _ := setupTestContext()
	entries := []registry.Entry{{ID: registry.NewID("app", "service"), Kind: registry.EntryKind, Data: payload.New(map[string]any{})}}
	for _, name := range []string{"second", "first"} {
		entries = append(entries, registry.Entry{ID: registry.NewID("app", name), Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{"targets": []any{map[string]any{"entry": "service", "path": ".config." + name}}})})
	}
	err := Link(WithStrictRequirements()).Execute(ctx, &entries)
	var diagnostic apierror.Error
	require.ErrorAs(t, err, &diagnostic)
	require.Equal(t, apierror.Invalid, diagnostic.Kind())
	require.Equal(t, apierror.False, diagnostic.Retryable())
	require.Equal(t, 2, diagnostic.Details().GetInt("count", 0))
	require.Len(t, diagnostic.Details().GetSlice("errors"), 2)
	rows, ok := diagnostic.Details().Get("requirements")
	require.True(t, ok)
	require.Equal(t, []any{
		map[string]any{"requirement": "app:first", "targets": []any{map[string]any{"entry": "service", "path": ".config.first"}}},
		map[string]any{"requirement": "app:second", "targets": []any{map[string]any{"entry": "service", "path": ".config.second"}}},
	}, rows)
}

func TestUnresolvedRequirementsKeepInternalFailuresInternal(t *testing.T) {
	err := NewUnresolvedRequirementsError([]error{
		NewRequirementError("audience", "app", ErrNoValueAvailable),
		NewRequirementTargetError("app:audience", "app:service", ".config.audience", apierror.New(apierror.Internal, "transcoder fails")),
	})
	require.Equal(t, apierror.Internal, err.Kind())
}
