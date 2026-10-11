// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
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
			Meta: attrs.Bag{"description": "Select the audience", "value_kind": "security.scope",
				"options": []any{"app:read", "app:write"}, "applicability": map[string]any{"mode": "review"}},
			Data: payload.New(map[string]any{"targets": []any{map[string]any{"entry": "service", "path": ".config.audience"}},
				"options": map[string]any{"allow_custom": false}})},
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
		local declaration = details.requirements[1].declaration
		assert(type(declaration) == "table")
		assert(declaration.id == "app:audience" and declaration.kind == "ns.requirement")
		assert(declaration.meta.description == "Select the audience")
		assert(declaration.meta.value_kind == "security.scope")
		assert(declaration.meta.options[1] == "app:read" and declaration.meta.options[2] == "app:write")
		assert(declaration.meta.applicability.mode == "review")
		assert(declaration.data.default == nil)
		assert(declaration.data.targets[1].entry == "service")
		assert(declaration.data.targets[1].path == ".config.audience")
		assert(declaration.data.options.allow_custom == false)
	`)
}
