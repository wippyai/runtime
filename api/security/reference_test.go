// SPDX-License-Identifier: MPL-2.0

package security_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	securitysys "github.com/wippyai/runtime/system/security"
)

type referencePolicy struct {
	id registry.ID
}

func (p referencePolicy) ID() registry.ID { return p.id }
func (p referencePolicy) Evaluate(security.Actor, string, string, attrs.Bag) security.Result {
	return security.Allow
}

type nonReferenceablePolicy struct {
	security.Policy
}

func (p nonReferenceablePolicy) ReferenceID() (registry.ID, error) {
	return registry.ID{}, security.ErrPolicyNotReferenceable
}

type nonReferenceableScope struct {
	security.Scope
}

func (s nonReferenceableScope) ReferenceIDs() ([]registry.ID, error) {
	return nil, security.ErrPolicyNotReferenceable
}

func TestPolicyReferenceID(t *testing.T) {
	policy := referencePolicy{id: registry.NewID("app", "read")}
	id, err := security.PolicyReferenceID(policy)
	require.NoError(t, err)
	require.Equal(t, policy.ID(), id)
	id, err = security.PolicyReferenceID(nonReferenceablePolicy{Policy: policy})
	require.ErrorIs(t, err, security.ErrPolicyNotReferenceable)
	require.Equal(t, registry.ID{}, id)
	id, err = security.PolicyReferenceID(nil)
	require.ErrorIs(t, err, security.ErrPolicyNotFound)
	require.Equal(t, registry.ID{}, id)
}

func TestPolicyReferenceIDs(t *testing.T) {
	policy := referencePolicy{id: registry.NewID("app", "read")}
	ids, err := security.PolicyReferenceIDs(securitysys.NewScope([]security.Policy{policy}))
	require.NoError(t, err)
	require.Equal(t, []registry.ID{policy.ID()}, ids)
	ids, err = security.PolicyReferenceIDs(nil)
	require.NoError(t, err)
	require.Empty(t, ids)
	ids, err = security.PolicyReferenceIDs(securitysys.NewScope(nil))
	require.NoError(t, err)
	require.Empty(t, ids)
	ids, err = security.PolicyReferenceIDs(nonReferenceableScope{Scope: securitysys.NewScope(nil)})
	require.ErrorIs(t, err, security.ErrPolicyNotReferenceable)
	require.Nil(t, ids)
	ids, err = security.PolicyReferenceIDs(securitysys.NewScope([]security.Policy{
		policy, nonReferenceablePolicy{Policy: referencePolicy{id: registry.NewID("app", "write")}},
	}))
	require.ErrorIs(t, err, security.ErrPolicyNotReferenceable)
	require.Nil(t, ids, "a rejected scope must never return partial authority")
}
