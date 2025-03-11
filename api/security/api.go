package security

import (
	"errors"
	"github.com/ponyruntime/pony/api/registry"
)

// Result represents a policy decision
type Result int

const (
	Undefined Result = iota
	Allow
	Deny
)

var (
	// ErrPolicyNotFound is returned when a requested policy does not exist
	ErrPolicyNotFound = errors.New("policy not found")

	// ErrGroupNotFound is returned when a requested policy group does not exist
	ErrGroupNotFound = errors.New("policy group not found")
)

type (
	// Actor represents a security principal (user, service, system process)
	Actor interface {
		// ID returns the unique identifier of the actor
		ID() string

		// Meta returns additional metadata about the actor
		Meta() registry.Metadata
	}

	// Policy defines an authorization policy
	Policy interface {
		// ID returns the policy's unique identifier
		ID() registry.ID

		// Evaluate determines if the action on resource is allowed/denied
		// The meta can be used to evaluate complex conditions
		Evaluate(actor Actor, action, resource string, meta registry.Metadata) Result
	}

	// Scope is an immutable collection of policies defining access boundaries
	Scope interface {
		// With returns a new Scope with the added policy
		With(policy Policy) Scope

		// Without returns a new Scope without the specified policy
		Without(policyID registry.ID) Scope

		// Evaluate checks all policies and determines if action is allowed
		Evaluate(actor Actor, action, resource string, meta registry.Metadata) Result

		// Contains checks if a policy is in the scope
		Contains(policyID registry.ID) bool

		// Policies returns all policies in the scope
		Policies() []Policy
	}

	// Registry defines the core interface for accessing security policies
	Registry interface {
		// GetPolicy retrieves a policy by its ID
		GetPolicy(id registry.ID) (Policy, error)

		// GetPolicyGroup retrieves all policies in a group as a scope
		GetPolicyGroup(groupID registry.ID) (Scope, error)

		// GetPoliciesByTag retrieves all policies with the specified tag
		GetPoliciesByTag(tag string) ([]Policy, error)

		// ListGroups returns all available policy group IDs
		ListGroups() []registry.ID

		// ListPolicies returns all available policy IDs
		ListPolicies() []registry.ID
	}
)
