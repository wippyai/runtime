// SPDX-License-Identifier: MPL-2.0

package tokenstore

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	cfg "github.com/wippyai/runtime/api/service/security/policy"
	impl "github.com/wippyai/runtime/service/security/policy"
	secsystem "github.com/wippyai/runtime/system/security"
)

func TestCompiledScopePreservesRestrictionCeiling(t *testing.T) {
	makePolicy := func(name, action string, effect cfg.Effect) security.Policy {
		p, err := impl.NewPolicy(registry.NewID("test", name), &cfg.Config{Policy: cfg.Definition{Actions: action, Resources: "*", Effect: effect}})
		require.NoError(t, err)
		return p
	}
	authority := makePolicy("authority", "read", cfg.Allow)
	ceiling := secsystem.NewScope([]security.Policy{makePolicy("ceiling", "write", cfg.Deny)})
	wrapped := restrictedPolicy{Policy: authority, ceiling: ceiling}
	_, described := any(wrapped).(security.PolicyDescriptor)
	require.False(t, described)
	// The wrapped authority is Undefined for write; the independent ceiling denies.
	s := secsystem.NewScope([]security.Policy{wrapped, makePolicy("unrelated-allow", "write", cfg.Allow)})
	for i := 0; i < 3; i++ {
		require.Equal(t, security.Deny, s.Evaluate(security.Actor{}, "write", "entry", nil))
	}
	foreign := &restrictedScope{authority: secsystem.NewScope([]security.Policy{authority}), ceiling: ceiling}
	require.Equal(t, security.Deny, foreign.With(makePolicy("addition", "write", cfg.Allow)).Evaluate(security.Actor{}, "write", "entry", nil))
	copied := secsystem.NewScope(foreign.Policies())
	require.Equal(t, security.Deny, copied.Evaluate(security.Actor{}, "write", "entry", nil))
}
