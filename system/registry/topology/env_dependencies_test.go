// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

func TestEnvironmentDependenciesResolveDeclaredNames(t *testing.T) {
	resolver := NewResolver()
	require.NoError(t, resolver.RegisterPattern(registry.DependencyPattern{Path: "data.*_env", AllowWildcard: true}))
	variable := registry.Entry{ID: registry.NewID("settings", "signing"), Kind: "env.variable", Data: payload.New(map[string]any{"variable": "SIGNING_KEY", "storage": "settings:os"})}
	for _, data := range []map[string]any{
		{"token_key_env": "SIGNING_KEY"},
		{"nested": map[string]any{"password_env": "SIGNING_KEY"}},
		{"url": "prefix-${env:SIGNING_KEY}"},
		{"values": []any{map[string]any{"secret_env": "SIGNING_KEY"}}},
	} {
		consumer := registry.Entry{ID: registry.NewID("app", "consumer"), Data: payload.New(data)}
		state := NewStateMap(registry.State{consumer, variable})
		require.Equal(t, []registry.ID{variable.ID}, ResolveDependencies(state, resolver)[consumer.ID])
		index := BuildDepIndex(registry.State{consumer, variable}, resolver)
		var targets []registry.ID
		require.NoError(t, index.VisitDependencies(state, nil, resolver, func(source, target registry.ID) error {
			if source == consumer.ID {
				targets = append(targets, target)
			}
			return nil
		}))
		require.Equal(t, []registry.ID{variable.ID}, targets)
		dependents := map[registry.ID]struct{}{}
		index.Dependents(variable, dependents)
		require.Contains(t, dependents, consumer.ID)
	}
}

func TestEnvironmentDependenciesTrackVariableRenames(t *testing.T) {
	consumer := registry.Entry{ID: registry.NewID("app", "consumer"), Data: payload.New(map[string]any{"key_env": "NEXT_KEY"})}
	variable := registry.Entry{ID: registry.NewID("settings", "key"), Kind: "env.variable", Data: payload.New(map[string]any{"variable": "OLD_KEY"})}
	baseline := registry.State{consumer, variable}
	index := BuildDepIndex(baseline, nil)
	renamed := variable
	renamed.Data = payload.New(map[string]any{"variable": "NEXT_KEY"})
	candidate := NewStateMap(registry.State{consumer, renamed})
	changes := registry.ChangeSet{{Kind: registry.EntryUpdate, Entry: renamed, OriginalEntry: &variable}}
	var targets []registry.ID
	require.NoError(t, index.VisitDependencies(candidate, changes, nil, func(source, target registry.ID) error {
		if source == consumer.ID {
			targets = append(targets, target)
		}
		return nil
	}))
	require.Equal(t, []registry.ID{variable.ID}, targets)
	index.Patch(changes, nil)
	dependents := map[registry.ID]struct{}{}
	index.Dependents(renamed, dependents)
	require.Contains(t, dependents, consumer.ID)
	dependents = map[registry.ID]struct{}{}
	index.Dependents(variable, dependents)
	require.Empty(t, dependents)
}

func TestEnvironmentDependenciesPreserveIDReferencesAndIgnoreUnresolvedNames(t *testing.T) {
	variable := registry.Entry{ID: registry.NewID("app", "key"), Kind: "env.variable", Data: payload.New(map[string]any{"variable": "DECLARED_KEY"})}
	for _, name := range []string{"app:key", "key", "DECLARED_KEY"} {
		consumer := registry.Entry{ID: registry.NewID("app", "consumer"), Data: payload.New(map[string]any{"key_env": name})}
		state := NewStateMap(registry.State{variable, consumer})
		require.Equal(t, []registry.ID{variable.ID}, ResolveDependencies(state, nil)[consumer.ID])
	}
	consumer := registry.Entry{ID: registry.NewID("app", "consumer"), Data: payload.New(map[string]any{"key_env": "MISSING", "literal": "$${env:DECLARED_KEY}"})}
	require.Empty(t, ResolveDependencies(NewStateMap(registry.State{variable, consumer}), nil)[consumer.ID])
}

func TestEnvironmentDependenciesIndexRemovesConsumerReferences(t *testing.T) {
	variable := registry.Entry{ID: registry.NewID("env", "key"), Kind: "env.variable", Data: payload.New(map[string]any{"variable": "KEY"})}
	consumer := registry.Entry{ID: registry.NewID("app", "consumer"), Data: payload.New(map[string]any{"key_env": "KEY"})}
	index := BuildDepIndex(registry.State{variable, consumer}, nil)
	next := consumer
	next.Data = payload.New(map[string]any{"key": "inline"})
	index.OnUpdate(consumer, next, nil)
	dependents := map[registry.ID]struct{}{}
	index.Dependents(variable, dependents)
	require.Empty(t, dependents)
	index.OnUpdate(next, consumer, nil)
	index.OnDelete(consumer)
	index.Dependents(variable, dependents)
	require.Empty(t, dependents)
}
