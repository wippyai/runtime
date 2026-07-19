// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

const (
	fixtureComponent = "example/accounts"
	fixtureNamespace = "identity.account"
)

func TestLinkUsesDeclaredModuleNamespace(t *testing.T) {
	tests := []struct {
		name      string
		parameter string
		want      string
	}{
		{name: "bare name", parameter: "router", want: "app:configured"},
		{name: "canonical requirement id", parameter: "identity.account.api:router", want: "app:configured"},
		{name: "package-derived namespace is not an address", parameter: "example.accounts:router", want: "app:default"},
		{name: "foreign requirement id is not an address", parameter: "analytics.report:router", want: "app:default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := setupTestContext()
			entries := declaredNamespaceFixture(tt.parameter)

			err := Link().Execute(ctx, &entries)
			require.NoError(t, err)

			target := findEntry(entries, "app", "endpoint")
			require.NotNil(t, target)
			assert.Equal(t, tt.want, target.Meta["router"])
		})
	}
}

func TestLinkDeclaredNamespaceOwnsChildrenOnly(t *testing.T) {
	ctx, _ := setupTestContext()
	entries := []registry.Entry{
		moduleDefinition(fixtureComponent, fixtureNamespace),
		moduleDefinition("example/reports", "analytics.report"),
		dependencyEntry(fixtureComponent, "identity.account.api:router", "app:account"),
		requirementEntry(fixtureComponent, "identity.account.api", "router", "app:account_endpoint", "app:default"),
		requirementEntry("example/reports", "analytics.report", "router", "app:report_endpoint", "app:report_default"),
		targetEntry("account_endpoint"),
		targetEntry("report_endpoint"),
	}

	err := Link().Execute(ctx, &entries)
	require.NoError(t, err)
	assert.Equal(t, "app:account", findEntry(entries, "app", "account_endpoint").Meta["router"])
	assert.Equal(t, "app:report_default", findEntry(entries, "app", "report_endpoint").Meta["router"])
}

func TestLinkRejectsLoadedModuleWithoutDefinition(t *testing.T) {
	ctx, _ := setupTestContext()
	entries := []registry.Entry{
		dependencyEntry(fixtureComponent, "router", "app:configured"),
		requirementEntry(fixtureComponent, fixtureNamespace, "router", "app:endpoint", "app:default"),
		targetEntry("endpoint"),
	}

	err := Link().Execute(ctx, &entries)
	require.Error(t, err)
	assert.ErrorContains(t, err, "loaded module has no ns.definition root namespace")
}

func TestLinkAllowsUnloadedDependencyDuringSourceBuild(t *testing.T) {
	ctx, _ := setupTestContext()
	entries := []registry.Entry{
		dependencyEntry(fixtureComponent, "router", "app:configured"),
	}

	require.NoError(t, Link().Execute(ctx, &entries))
}

func TestLinkRejectsConflictingModuleDefinitions(t *testing.T) {
	tests := []struct {
		name    string
		entries []registry.Entry
		want    string
	}{
		{
			name: "one module declares two roots",
			entries: []registry.Entry{
				moduleDefinition(fixtureComponent, fixtureNamespace),
				moduleDefinition(fixtureComponent, "identity.profile"),
			},
			want: "declares multiple namespaces",
		},
		{
			name: "two modules declare one root",
			entries: []registry.Entry{
				moduleDefinition(fixtureComponent, fixtureNamespace),
				moduleDefinition("example/profiles", fixtureNamespace),
			},
			want: "declared by multiple modules",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := setupTestContext()
			err := Link().Execute(ctx, &tt.entries)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func declaredNamespaceFixture(parameter string) []registry.Entry {
	return []registry.Entry{
		moduleDefinition(fixtureComponent, fixtureNamespace),
		dependencyEntry(fixtureComponent, parameter, "app:configured"),
		requirementEntry(fixtureComponent, "identity.account.api", "router", "app:endpoint", "app:default"),
		targetEntry("endpoint"),
	}
}

func moduleDefinition(component, namespace string) registry.Entry {
	return registry.Entry{
		ID:   registry.NewID(namespace, "definition"),
		Kind: registry.NamespaceDefinition,
		Meta: map[string]any{"module": component},
	}
}

func dependencyEntry(component, parameter string, value any) registry.Entry {
	return registry.Entry{
		ID:   registry.NewID("app.dependencies", "module"),
		Kind: registry.NamespaceDependency,
		Data: payload.New(map[string]any{
			"component": component,
			"parameters": []any{
				map[string]any{"name": parameter, "value": value},
			},
		}),
	}
}

func requirementEntry(component, namespace, name, target string, defaultValue any) registry.Entry {
	return registry.Entry{
		ID:   registry.NewID(namespace, name),
		Kind: registry.NamespaceRequirement,
		Meta: map[string]any{"module": component},
		Data: payload.New(map[string]any{
			"default": defaultValue,
			"targets": []any{
				map[string]any{"entry": target, "path": ".meta.router"},
			},
		}),
	}
}

func targetEntry(name string) registry.Entry {
	return registry.Entry{
		ID:   registry.NewID("app", name),
		Kind: "http.endpoint",
		Meta: map[string]any{},
		Data: payload.New(map[string]any{}),
	}
}
