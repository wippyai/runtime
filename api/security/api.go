package security

import (
	"github.com/ponyruntime/pony/api/registry"
	"time"
)

// Result represents a policy decision
type Result int

const (
	Undefined Result = iota
	Allow
	Deny
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
		ID() string

		// Evaluate determines if the action on resource is allowed/denied
		// The ctx can be used to evaluate complex conditions
		Evaluate(actor Actor, action, resource string, meta registry.Metadata) Result
	}

	// PolicySet is an immutable collection of policies
	PolicySet interface {
		// With returns a new PolicySet with the added policy
		With(policy Policy) PolicySet

		// Without returns a new PolicySet without the specified policy
		Without(policyID string) PolicySet

		// Evaluate checks all policies and determines if action is allowed
		Evaluate(actor Actor, action, resource string, meta registry.Metadata) Result

		// Contains checks if a policy is in the set
		Contains(policyID string) bool

		// Policies returns all policies in the set
		Policies() []Policy
	}

	// TokenInfo contains information about an authentication token
	TokenInfo struct {
		// ActorID identifies the actor associated with this token
		ActorID string

		// Expiry defines when this token expires
		Expiry time.Time

		// Scope defines what this token can be used for
		Scope string

		// Extra contains additional application-specific data
		Extra map[string]any
	}
)
