// Package std provides standard workflow command types that are implementation-agnostic.
// These types define the contract between workflow code and execution environments
// (Temporal, DAG-based, etc.).
package std

import "github.com/wippyai/runtime/api/registry"

// Command type constants define the standard workflow command types.
// These are used as the Type() value in runtime.Command.
const (
	// TypeFuncsCall represents a function invocation command.
	// Maps to: Function call, Temporal Activity
	TypeFuncsCall = "funcs.call"

	// TypeContractCall represents a contract method invocation command.
	// Maps to: Contract binding method call
	TypeContractCall = "contract.call"

	// TypeTimerSleep represents a timer/sleep command.
	// Maps to: Temporal Timer, simple delay
	TypeTimerSleep = "timer.sleep"

	// TypeProcessSend represents an inter-process message send command.
	// Maps to: Process messaging, Temporal Signal
	TypeProcessSend = "process.send"

	// TypeChildWorkflow represents a child workflow spawn command.
	// Maps to: Temporal Child Workflow
	TypeChildWorkflow = "workflow.child"
)

// SecurityContext contains security information for command execution.
// This is reusable across all command headers that need security context.
type SecurityContext struct {
	// ActorID is the security actor's unique identifier.
	ActorID string `json:"actor_id,omitempty"`

	// ActorMeta contains additional metadata about the actor.
	ActorMeta registry.Metadata `json:"actor_meta,omitempty"`

	// ScopePolicies contains the IDs of policies in the security scope.
	// Host reconstructs the Scope from these policy IDs via security.Registry.
	ScopePolicies []registry.ID `json:"scope_policies,omitempty"`
}

// RetryPolicy defines retry behavior for operations.
type RetryPolicy struct {
	// MaxAttempts is the maximum number of attempts (including initial).
	// 0 means unlimited.
	MaxAttempts int `json:"max_attempts,omitempty"`

	// InitialInterval is the initial retry interval as duration string (e.g., "1s").
	InitialInterval string `json:"initial_interval,omitempty"`

	// BackoffCoefficient is the multiplier for each retry interval.
	BackoffCoefficient float64 `json:"backoff_coefficient,omitempty"`

	// MaxInterval is the maximum retry interval as duration string.
	MaxInterval string `json:"max_interval,omitempty"`

	// NonRetryableErrors lists error types that should not be retried.
	NonRetryableErrors []string `json:"non_retryable_errors,omitempty"`
}
