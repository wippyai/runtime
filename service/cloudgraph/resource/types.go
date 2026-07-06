// SPDX-License-Identifier: MPL-2.0

// Package resource holds the cloud-graph domain types shared by the planner,
// executor, workflow, and ledger (docs/design/cloud-graph.md §5).
package resource

import "encoding/json"

// Action is the operation an execution plan schedules for a resource.
// The PoC supports create only; update/delete/replace are reserved.
type Action string

// ActionCreate provisions a resource that does not exist yet.
const ActionCreate Action = "create"

// DeployStatus is the deploy state machine (design §9.3, PoC subset without
// rolling_back and cancelled).
type DeployStatus string

// Deploy statuses.
const (
	DeployPlanning  DeployStatus = "planning"
	DeployExecuting DeployStatus = "executing"
	DeploySucceeded DeployStatus = "succeeded"
	DeployFailed    DeployStatus = "failed"
	DeployPartial   DeployStatus = "partial"
)

// Terminal reports whether the deploy status is terminal.
func (s DeployStatus) Terminal() bool {
	return s == DeploySucceeded || s == DeployFailed || s == DeployPartial
}

// OperationStatus is the operation state machine (design §9.5, PoC subset
// without acknowledged/observed/compensating/cancelled).
type OperationStatus string

// Operation statuses.
const (
	OpPlanned    OperationStatus = "planned"
	OpDispatched OperationStatus = "dispatched"
	OpInProgress OperationStatus = "in_progress"
	OpCommitted  OperationStatus = "committed"
	OpFailed     OperationStatus = "failed"
)

// Terminal reports whether the operation status is terminal.
func (s OperationStatus) Terminal() bool {
	return s == OpCommitted || s == OpFailed
}

// DependencyKind is the closed set of dependency edge kinds (design §5.4).
type DependencyKind string

// Dependency kinds.
const (
	DepCreate     DependencyKind = "create"
	DepConfigure  DependencyKind = "configure"
	DepHealth     DependencyKind = "health"
	DepOrdering   DependencyKind = "ordering"
	DepPlacement  DependencyKind = "placement"
	DepAttachment DependencyKind = "attachment"
	DepOwnership  DependencyKind = "ownership"
	DepTraffic    DependencyKind = "traffic"
	DepSoft       DependencyKind = "soft"
)

// Valid reports whether the kind belongs to the closed set.
func (k DependencyKind) Valid() bool {
	switch k {
	case DepCreate, DepConfigure, DepHealth, DepOrdering, DepPlacement,
		DepAttachment, DepOwnership, DepTraffic, DepSoft:
		return true
	}
	return false
}

// DependencyRef declares one typed dependency edge from the declaring spec
// onto a target resource in the same scope.
type DependencyRef struct {
	Target string         `json:"target"`
	Kind   DependencyKind `json:"kind"`
}

// Spec is one declared resource inside a deploy (design §5.2, PoC
// subset without mounts, secrets, placement, and watcher config).
type Spec struct {
	ResourceID   string                   `json:"resource_id"`
	Type         string                   `json:"type"`
	ProviderType string                   `json:"provider_type"`
	Dependencies map[string]DependencyRef `json:"dependencies,omitempty"`
	Desired      json.RawMessage          `json:"desired"`
}

// DeployInput is the deploy request submitted through submit_intent
// (design §6.1, full mode only).
type DeployInput struct {
	DeployID string `json:"deploy_id"`
	Scope    string `json:"scope"`
	Specs    []Spec `json:"specs"`
}

// DeployRecord is the persisted state of one deploy.
type DeployRecord struct {
	DeployID     string       `json:"deploy_id"`
	Scope        string       `json:"scope"`
	Status       DeployStatus `json:"status"`
	Error        string       `json:"error,omitempty"`
	FinalizedSeq int64        `json:"finalized_seq,omitempty"`
}

// Operation is one persisted operation row.
type Operation struct {
	ID          string          `json:"id"`
	DeployID    string          `json:"deploy_id"`
	ResourceID  string          `json:"resource_id"`
	Action      Action          `json:"action"`
	Status      OperationStatus `json:"status"`
	ActorPID    string          `json:"actor_pid,omitempty"`
	Error       string          `json:"error,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Incarnation int64           `json:"incarnation"`
	LastSignal  int64           `json:"last_signal_seq"`
}

// Edge is one persisted dependency edge scoped to a deploy.
type Edge struct {
	SourceResourceID string         `json:"source"`
	TargetResourceID string         `json:"target"`
	Kind             DependencyKind `json:"kind"`
}

// SignalKind classifies a provider signal envelope.
type SignalKind string

// Signal kinds.
const (
	SignalPhase      SignalKind = "phase"
	SignalOutput     SignalKind = "output"
	SignalCheckpoint SignalKind = "checkpoint"
	SignalFailed     SignalKind = "failed"
)

// SignalPhaseName is the closed, exhaustive provider phase set (design I2).
type SignalPhaseName string

// Provider signal phases.
const (
	PhaseAllocateStarted    SignalPhaseName = "allocate_started"
	PhaseAllocateCommitted  SignalPhaseName = "allocate_committed"
	PhaseConfigureStarted   SignalPhaseName = "configure_started"
	PhaseConfigureCommitted SignalPhaseName = "configure_committed"
	PhaseVerifyStarted      SignalPhaseName = "verify_started"
	PhaseVerifyPassed       SignalPhaseName = "verify_passed"
	PhaseVerifyFailed       SignalPhaseName = "verify_failed"
	PhaseDeleteStarted      SignalPhaseName = "delete_started"
	PhaseDeleteCommitted    SignalPhaseName = "delete_committed"
)

// ValidPhase reports whether the phase belongs to the closed set.
func ValidPhase(p SignalPhaseName) bool {
	switch p {
	case PhaseAllocateStarted, PhaseAllocateCommitted, PhaseConfigureStarted,
		PhaseConfigureCommitted, PhaseVerifyStarted, PhaseVerifyPassed,
		PhaseVerifyFailed, PhaseDeleteStarted, PhaseDeleteCommitted:
		return true
	}
	return false
}

// Signal is one provider signal envelope traveling through the durable
// mailbox (design §10.2).
type Signal struct {
	OperationID string          `json:"operation_id"`
	Kind        SignalKind      `json:"kind"`
	Phase       SignalPhaseName `json:"phase,omitempty"`
	Payload     json.RawMessage `json:"payload"`
	Incarnation int64           `json:"incarnation"`
	Seq         int64           `json:"seq"`
}

// Instance is the persisted resource instance state (design §5.3, PoC subset
// of the five axes: lifecycle only).
type Instance struct {
	ResourceID   string          `json:"resource_id"`
	Scope        string          `json:"scope"`
	Type         string          `json:"type"`
	ProviderType string          `json:"provider_type"`
	Lifecycle    string          `json:"lifecycle"`
	Outputs      json.RawMessage `json:"outputs,omitempty"`
	Generation   int64           `json:"generation"`
	OutputEpoch  int64           `json:"output_epoch"`
}
