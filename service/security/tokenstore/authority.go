// SPDX-License-Identifier: MPL-2.0

package tokenstore

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	securitysys "github.com/wippyai/runtime/system/security"
)

type subjectAuthority struct {
	Ceiling   *authorityScope   `json:"ceiling"`
	Error     *subjectError     `json:"error"`
	Meta      authorityMetadata `json:"meta"`
	Success   *bool             `json:"success"`
	SubjectID string            `json:"subject_id"`
	Groups    []registry.ID     `json:"groups"`
	Retriable bool              `json:"retriable"`
}

// A Lua empty table encodes as [] unless it has string keys. Accept that empty
// representation only at these object-shaped lookup fields, not in global JSON.
type authorityMetadata attrs.Bag

func (m *authorityMetadata) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("[]")) {
		*m = authorityMetadata{}
		return nil
	}
	return json.Unmarshal(data, (*attrs.Bag)(m))
}

type subjectError struct {
	Message string        `json:"message"`
	Kind    apierror.Kind `json:"kind"`
}

type authorityScope struct {
	Groups   []registry.ID `json:"groups"`
	Policies []registry.ID `json:"policies"`
}

func (s *authorityScope) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("[]")) {
		data = []byte("{}")
	}
	type scopeJSON authorityScope
	return json.Unmarshal(data, (*scopeJSON)(s))
}

func (s *TokenStore) resolveSubject(ctx context.Context, data *tokenData) (security.Actor, security.Scope, error) {
	funcs := function.GetRegistry(ctx)
	if funcs == nil {
		return security.Actor{}, nil, function.ErrRegistryNotFound
	}
	result, err := funcs.Call(ctx, runtimeapi.Task{
		ID: s.config.SubjectLookup,
		Payloads: payload.Payloads{payload.New(attrs.Bag{
			"subject_id":     data.ActorID,
			"authority_only": true,
			"actor_meta":     data.ActorMeta,
			"token_meta":     data.Meta,
		})},
	})
	if err != nil {
		return security.Actor{}, nil, err
	}
	if result == nil {
		return security.Actor{}, nil, invalidAuthority("subject lookup returns no result")
	}
	if result.Error != nil {
		return security.Actor{}, nil, result.Error
	}
	if result.Value == nil {
		return security.Actor{}, nil, invalidAuthority("subject lookup returns no authority")
	}
	var authority subjectAuthority
	if err := s.dtt.Unmarshal(result.Value, &authority); err != nil {
		return security.Actor{}, nil, NewUnmarshalTokenDataError(err)
	}
	if authority.Error != nil {
		retryable := apierror.False
		if authority.Retriable {
			retryable = apierror.True
		}
		return security.Actor{}, nil, apierror.E(authority.Error.Kind, authority.Error.Message, retryable, nil, nil)
	}
	if authority.Success != nil && !*authority.Success {
		return security.Actor{}, nil, security.ErrPermissionDenied
	}
	if data.ActorID == "" || authority.SubjectID != data.ActorID {
		return security.Actor{}, nil, invalidAuthority("subject lookup returns a different subject")
	}
	if authority.Groups == nil {
		return security.Actor{}, nil, invalidAuthority("subject lookup omits groups")
	}
	scope, err := s.authorityScope(authorityScope{Groups: authority.Groups})
	if err != nil {
		return security.Actor{}, nil, err
	}
	if authority.Ceiling != nil {
		ceiling, err := s.authorityScope(*authority.Ceiling)
		if err != nil {
			return security.Actor{}, nil, err
		}
		scope = &restrictedScope{authority: scope, ceiling: ceiling}
	}
	return security.Actor{ID: data.ActorID, Meta: attrs.Bag(authority.Meta)}, scope, nil
}

func invalidAuthority(message string) error {
	return apierror.New(apierror.Invalid, message).WithRetryable(apierror.False)
}

func (s *TokenStore) authorityScope(config authorityScope) (security.Scope, error) {
	return securitysys.ResolveScope(s.registry, config.Groups, config.Policies)
}

// restrictedScope keeps a credential ceiling across scope copies and policy edits.
type restrictedScope struct {
	authority security.Scope
	ceiling   security.Scope
}

func (s *restrictedScope) ReferenceIDs() ([]registry.ID, error) {
	return nil, security.ErrPolicyNotReferenceable
}

func (s *restrictedScope) With(policy security.Policy) security.Scope {
	return &restrictedScope{authority: s.authority.With(policy), ceiling: s.ceiling}
}
func (s *restrictedScope) Without(id registry.ID) security.Scope {
	return &restrictedScope{authority: s.authority.Without(id), ceiling: s.ceiling}
}
func (s *restrictedScope) Contains(id registry.ID) bool { return s.authority.Contains(id) }
func (s *restrictedScope) Evaluate(actor security.Actor, action, resource string, meta attrs.Bag) security.Result {
	return intersectDecisions(s.authority.Evaluate(actor, action, resource, meta), s.ceiling.Evaluate(actor, action, resource, meta))
}
func (s *restrictedScope) Policies() []security.Policy {
	policies := s.authority.Policies()
	for i, policy := range policies {
		policies[i] = restrictedPolicy{Policy: policy, ceiling: s.ceiling}
	}
	return policies
}

type restrictedPolicy struct {
	security.Policy
	ceiling security.Scope
}

// Reconstructing this policy from ID alone would discard its credential ceiling.
func (p restrictedPolicy) ReferenceID() (registry.ID, error) {
	return registry.ID{}, security.ErrPolicyNotReferenceable
}

func (p restrictedPolicy) Evaluate(actor security.Actor, action, resource string, meta attrs.Bag) security.Result {
	return intersectDecisions(p.Policy.Evaluate(actor, action, resource, meta), p.ceiling.Evaluate(actor, action, resource, meta))
}
func intersectDecisions(authority, ceiling security.Result) security.Result {
	if authority == security.Deny || ceiling == security.Deny {
		return security.Deny
	}
	if authority == security.Allow && ceiling == security.Allow {
		return security.Allow
	}
	return security.Undefined
}
