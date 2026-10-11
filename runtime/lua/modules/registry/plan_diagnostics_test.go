// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	lua "github.com/wippyai/go-lua"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build"
	"github.com/wippyai/runtime/boot/build/stages"
	"github.com/wippyai/runtime/boot/deps/hub"
	sysregistry "github.com/wippyai/runtime/system/registry"
)

type diagnosticPlanRegistry struct{ *mockRegistry }

func (r diagnosticPlanRegistry) Plan(ctx context.Context, _ regapi.Version, _ regapi.ChangeSet) (*regapi.Plan, error) {
	entries := []regapi.Entry{
		{ID: regapi.NewID("app", "audience"), Kind: regapi.NamespaceRequirement,
			Data: payload.New(map[string]any{"targets": []any{map[string]any{"entry": "service", "path": ".config.audience"}}})},
		{ID: regapi.NewID("app", "service"), Kind: regapi.EntryKind, Data: payload.New(map[string]any{})},
	}
	err := build.New(stages.Link(stages.WithStrictRequirements())).Execute(ctx, &entries)
	return nil, sysregistry.NewExpandChangesError(hub.NewDependencyPipelineError(err))
}

func TestChangesPlanPreservesNativeRequirementDiagnostic(t *testing.T) {
	lua.SetErrorMetadataExtractor(func(err error) *lua.ErrorMetadata {
		root := apierror.BuildChain(err).Root()
		return &lua.ErrorMetadata{Kind: lua.Kind(root.Kind), Retryable: root.Retryable, Details: root.Details}
	})
	t.Cleanup(func() { lua.SetErrorMetadataExtractor(nil) })
	reg := diagnosticPlanRegistry{planTestMock()}
	runRegistryLua(setupContextWithTranscoder(), t, reg, `
		local changes = assert(registry.snapshot()):changes()
		changes:create({id = "app:module", kind = "ns.dependency", data = {component = "example/module"}})
		local plan, err = changes:plan()
		assert(plan == nil and err ~= nil)
		assert(err:kind() == errors.INVALID, tostring(err))
		assert(err:retryable() == false)
		assert(string.find(err:message(), "unresolved requirements", 1, true))
		local details = err:details()
		assert(details.count == 1 and #details.errors == 1)
		assert(details.requirements[1].requirement == "app:audience")
		assert(details.requirements[1].targets[1].entry == "service")
		assert(details.requirements[1].targets[1].path == ".config.audience")
	`)
}
