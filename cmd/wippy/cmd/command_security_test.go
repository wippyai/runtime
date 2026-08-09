// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	"go.uber.org/zap"
)

type commandEntryRegistry struct {
	registry.Registry
	err   error
	entry registry.Entry
}

func (r *commandEntryRegistry) GetEntry(registry.ID) (registry.Entry, error) {
	return r.entry, r.err
}

type commandSecurityRegistry struct {
	secapi.Registry
	policies map[registry.ID]secapi.Policy
	groups   map[registry.ID]secapi.Scope
}

func (r *commandSecurityRegistry) GetPolicy(id registry.ID) (secapi.Policy, error) {
	policy, ok := r.policies[id]
	if !ok {
		return nil, secapi.ErrPolicyNotFound
	}
	return policy, nil
}

func (r *commandSecurityRegistry) GetPolicyGroup(id registry.ID) (secapi.Scope, error) {
	scope, ok := r.groups[id]
	if !ok {
		return nil, secapi.ErrGroupNotFound
	}
	return scope, nil
}

type commandPolicy struct{ id registry.ID }

func (p commandPolicy) ID() registry.ID { return p.id }
func (p commandPolicy) Evaluate(secapi.Actor, string, string, attrs.Bag) secapi.Result {
	return secapi.Allow
}

func TestExtractCommandMeta_Security(t *testing.T) {
	t.Run("no security block", func(t *testing.T) {
		meta, err := extractCommandMeta(map[string]any{
			"command": map[string]any{"name": "test"},
		})
		require.NoError(t, err)
		require.NotNil(t, meta)
		assert.Nil(t, meta.Security)
	})

	t.Run("actor metadata and policy references", func(t *testing.T) {
		meta, err := extractCommandMeta(map[string]any{
			"command": map[string]any{
				"name": "test",
				"security": map[string]any{
					"actor": map[string]any{
						"id":   "wippy.test:runner",
						"meta": map[string]any{"tenant": "acme"},
					},
					"policies": []any{"wippy.test:runner_policy"},
					"groups":   []any{"app.security:admin"},
				},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, meta)
		require.NotNil(t, meta.Security)
		assert.Equal(t, "wippy.test:runner", meta.Security.Actor.ID)
		assert.Equal(t, attrs.Bag{"tenant": "acme"}, meta.Security.Actor.Meta)
		assert.Equal(t, []registry.ID{registry.NewID("wippy.test", "runner_policy")}, meta.Security.Policies)
		assert.Equal(t, []registry.ID{registry.NewID("app.security", "admin")}, meta.Security.PolicyGroups)
	})

	t.Run("empty security block is rejected", func(t *testing.T) {
		meta, err := extractCommandMeta(map[string]any{
			"command": map[string]any{
				"name":     "test",
				"security": map[string]any{"actor": map[string]any{}},
			},
		})
		assert.Nil(t, meta)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "security configuration is empty")
	})

	t.Run("malformed policy entries fail decoding", func(t *testing.T) {
		meta, err := extractCommandMeta(map[string]any{
			"command": map[string]any{
				"name": "test",
				"security": map[string]any{
					"actor":    map[string]any{"id": "a"},
					"policies": []any{7},
				},
			},
		})
		assert.Nil(t, meta)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode command metadata")
	})

	t.Run("policy object rejects unknown ID fields", func(t *testing.T) {
		meta, err := extractCommandMeta(map[string]any{
			"command": map[string]any{
				"name": "test",
				"security": map[string]any{
					"actor": map[string]any{"id": "a"},
					"policies": []any{map[string]any{
						"ns":     "app",
						"name":   "runner",
						"secret": "must-reject",
					}},
				},
			},
		})
		assert.Nil(t, meta)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown field")
		assert.Contains(t, err.Error(), "secret")
	})

	t.Run("unknown security fields fail decoding", func(t *testing.T) {
		meta, err := extractCommandMeta(map[string]any{
			"command": map[string]any{
				"name": "test",
				"security": map[string]any{
					"actor":    map[string]any{"id": "a"},
					"polciies": []any{"app:runner"},
				},
			},
		})
		assert.Nil(t, meta)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown field")
	})

	t.Run("security requires a command name", func(t *testing.T) {
		meta, err := extractCommandMeta(map[string]any{
			"command": map[string]any{
				"security": map[string]any{"actor": map[string]any{"id": "a"}},
			},
		})
		assert.Nil(t, meta)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "security requires a command name")
	})

	t.Run("unknown command metadata remains compatible", func(t *testing.T) {
		meta, err := extractCommandMeta(map[string]any{
			"command": map[string]any{
				"name":   "test",
				"future": map[string]any{"enabled": true},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, meta)
		assert.Equal(t, "test", meta.Name)
	})
}

func TestResolveCommandSecurity_ProducesTerminalFrameContext(t *testing.T) {
	policyID := registry.NewID("app", "command")
	rootCtx := ctxapi.NewRootContext()
	rootCtx = registry.WithRegistry(rootCtx, &commandEntryRegistry{entry: registry.Entry{
		ID:   registry.NewID("app", "runner"),
		Kind: "process.lua",
		Meta: map[string]any{
			"command": map[string]any{
				"name": "runner",
				"security": map[string]any{
					"actor": map[string]any{
						"id":   "app:runner",
						"meta": map[string]any{"tenant": "acme"},
					},
					"policies": []any{policyID.String()},
				},
			},
		},
	}})
	rootCtx = secapi.WithRegistry(rootCtx, &commandSecurityRegistry{
		policies: map[registry.ID]secapi.Policy{policyID: commandPolicy{id: policyID}},
	})
	callerCtx, callerFrame := ctxapi.OpenFrameContext(rootCtx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(callerFrame) })

	pairs, err := resolveCommandSecurity(callerCtx, registry.NewID("app", "runner"))
	require.NoError(t, err)
	require.Len(t, pairs, 2)

	// Mirror terminal.Host.prepareContext: create a process frame and apply
	// process.Start.Context after inherited values.
	processCtx, processFrame := ctxapi.OpenFrameContextOn(context.Background(), callerCtx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(processFrame) })
	require.NoError(t, processFrame.SetMultiple(pairs...))

	actor, ok := secapi.GetActor(processCtx)
	require.True(t, ok)
	assert.Equal(t, "app:runner", actor.ID)
	assert.Equal(t, "acme", actor.Meta["tenant"])
	scope, ok := secapi.GetScope(processCtx)
	require.True(t, ok)
	assert.True(t, scope.Contains(policyID))
}

func TestResolveCommandSecurity_FailsClosed(t *testing.T) {
	t.Run("malformed declaration", func(t *testing.T) {
		rootCtx := registry.WithRegistry(ctxapi.NewRootContext(), &commandEntryRegistry{entry: registry.Entry{
			Meta: map[string]any{"command": map[string]any{
				"name":     "runner",
				"security": map[string]any{"policies": []any{7}},
			}},
		}})

		pairs, err := resolveCommandSecurity(rootCtx, registry.NewID("app", "runner"))
		assert.Nil(t, pairs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode command metadata")
	})

	t.Run("missing policy", func(t *testing.T) {
		policyID := registry.NewID("app", "missing")
		rootCtx := registry.WithRegistry(ctxapi.NewRootContext(), &commandEntryRegistry{entry: registry.Entry{
			Meta: map[string]any{"command": map[string]any{
				"name":     "runner",
				"security": map[string]any{"policies": []any{policyID.String()}},
			}},
		}})
		rootCtx = secapi.WithRegistry(rootCtx, &commandSecurityRegistry{policies: map[registry.ID]secapi.Policy{}})

		pairs, err := resolveCommandSecurity(rootCtx, registry.NewID("app", "runner"))
		assert.Nil(t, pairs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), policyID.String())
	})

	t.Run("missing group returns no partial pairs", func(t *testing.T) {
		groupID := registry.NewID("app", "missing-group")
		rootCtx := registry.WithRegistry(ctxapi.NewRootContext(), &commandEntryRegistry{entry: registry.Entry{
			Meta: map[string]any{"command": map[string]any{
				"name":     "runner",
				"security": map[string]any{"groups": []any{groupID.String()}},
			}},
		}})
		rootCtx = secapi.WithRegistry(rootCtx, &commandSecurityRegistry{groups: map[registry.ID]secapi.Scope{}})

		pairs, err := resolveCommandSecurity(rootCtx, registry.NewID("app", "runner"))
		assert.Nil(t, pairs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), groupID.String())
	})

	t.Run("mixed valid and missing policy references are atomic", func(t *testing.T) {
		validID := registry.NewID("app", "valid")
		missingID := registry.NewID("app", "missing")
		rootCtx := registry.WithRegistry(ctxapi.NewRootContext(), &commandEntryRegistry{entry: registry.Entry{
			Meta: map[string]any{"command": map[string]any{
				"name": "runner",
				"security": map[string]any{
					"policies": []any{validID.String(), missingID.String()},
				},
			}},
		}})
		rootCtx = secapi.WithRegistry(rootCtx, &commandSecurityRegistry{
			policies: map[registry.ID]secapi.Policy{validID: commandPolicy{id: validID}},
		})

		pairs, err := resolveCommandSecurity(rootCtx, registry.NewID("app", "runner"))
		assert.Nil(t, pairs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), missingID.String())
	})

	t.Run("entry lookup failure", func(t *testing.T) {
		rootCtx := registry.WithRegistry(ctxapi.NewRootContext(), &commandEntryRegistry{err: errors.New("not found")})
		pairs, err := resolveCommandSecurity(rootCtx, registry.NewID("app", "runner"))
		assert.Nil(t, pairs)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "get command entry")
	})
}

func TestLaunchExecProcess_RejectsInvalidSecurityBeforeStarting(t *testing.T) {
	rootCtx := registry.WithRegistry(ctxapi.NewRootContext(), &commandEntryRegistry{entry: registry.Entry{
		Meta: map[string]any{"command": map[string]any{
			"name":     "runner",
			"security": map[string]any{"policies": []any{7}},
		}},
	}})

	err := launchExecProcess(rootCtx, zap.NewNop(), "app:runner", "terminal", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve command security for app:runner")
	assert.NotContains(t, err.Error(), ErrProcessManagerNotAvailable.Error())
}
