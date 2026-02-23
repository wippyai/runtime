// SPDX-License-Identifier: MPL-2.0

// Package security provides security and authentication abstractions.
package security

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
)

// System identifies the security system in the event bus.
const System event.System = "security"

// Event kinds for policy operations.
const (
	PolicyRegister event.Kind = "policy.register"
	PolicyUpdate   event.Kind = "policy.update"
	PolicyDelete   event.Kind = "policy.delete"
)

// Result values for policy decisions.
const (
	Undefined Result = iota
	Allow
	Deny
)

type (
	// Result represents a policy decision.
	Result int

	// TokenType represents the type of token (e.g., JWT, opaque).
	TokenType string

	// Token represents a security token used for authentication.
	Token string

	// Config defines the security properties for a service.
	Config struct {
		Actor        Actor         `json:"actor" yaml:"actor"`
		PolicyGroups []registry.ID `json:"groups,omitempty" yaml:"groups,omitempty"`
		Policies     []registry.ID `json:"policies,omitempty" yaml:"policies,omitempty"`
	}

	// Actor represents a security principal (user, service, system process).
	Actor struct {
		Meta attrs.Bag `expr:"meta"`
		ID   string    `expr:"id"`
	}

	// TokenDetails defines options for token creation.
	TokenDetails struct {
		Meta       attrs.Bag
		Expiration time.Duration
	}

	// Policy defines an authorization policy.
	Policy interface {
		// ID returns the policy's unique identifier.
		ID() registry.ID

		// Evaluate determines if the action on resource is allowed/denied.
		Evaluate(actor Actor, action, resource string, meta attrs.Bag) Result
	}

	// Scope is an immutable collection of policies defining access boundaries.
	Scope interface {
		// With returns a new Scope with the added policy.
		With(policy Policy) Scope

		// Without returns a new Scope without the specified policy.
		Without(policyID registry.ID) Scope

		// Evaluate checks all policies and determines if action is allowed.
		Evaluate(actor Actor, action, resource string, meta attrs.Bag) Result

		// Contains checks if a policy is in the scope.
		Contains(policyID registry.ID) bool

		// Policies returns all policies in the scope.
		Policies() []Policy
	}

	// PolicyEntry represents a policy registration payload.
	PolicyEntry struct {
		Policy Policy
		Groups []registry.ID
	}

	// Registry defines the core interface for accessing security policies.
	Registry interface {
		// GetPolicy retrieves a policy by its ID.
		GetPolicy(id registry.ID) (Policy, error)

		// GetPolicyGroup retrieves all policies in a group as a scope.
		GetPolicyGroup(groupID registry.ID) (Scope, error)

		// ListGroups returns all available policy group IDs.
		ListGroups() []registry.ID

		// ListPolicies returns all available policy IDs.
		ListPolicies() []registry.ID
	}

	// TokenStore defines the interface for managing authentication tokens.
	TokenStore interface {
		// Create generates a new token for the given actor and scope.
		Create(ctx context.Context, actor Actor, scope Scope, details TokenDetails) (Token, error)

		// Validate checks if a token is valid and returns the associated actor and scope.
		Validate(ctx context.Context, token Token) (Actor, Scope, error)

		// Revoke removes a token from the store.
		Revoke(ctx context.Context, token Token) error
	}
)
