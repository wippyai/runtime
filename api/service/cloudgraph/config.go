// SPDX-License-Identifier: MPL-2.0

// Package cloudgraph defines the registry kinds and configuration for the
// cloud-graph deployment control plane (see docs/design/cloud-graph.md).
package cloudgraph

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

const (
	// Engine identifies the cloud-graph engine entry in the registry.
	Engine registry.Kind = "cloudgraph.engine"

	// Provider identifies a cloud-graph provider binding manifest in the registry.
	Provider registry.Kind = "cloudgraph.provider"
)

// DefaultWorkflowName is the Temporal workflow type the engine registers when
// the entry does not override it.
const DefaultWorkflowName = "cg_deploy"

// EngineConfig configures one cloud-graph engine instance (one shard).
type EngineConfig struct {
	// Meta holds arbitrary metadata associated with this engine entry.
	Meta attrs.Bag `json:"meta"`
	// Worker references the temporal.worker entry the deploy workflow and
	// activities register on.
	Worker registry.ID `json:"worker"`
	// Client references the temporal.client entry used to start deploy workflows.
	Client registry.ID `json:"client"`
	// Database references the db.sql.* entry backing the ledger.
	Database registry.ID `json:"database"`
	// Host references the process.host entry provider actors are spawned on.
	Host registry.ID `json:"host"`
	// Shard names the shard this engine owns.
	Shard string `json:"shard"`
	// WorkflowName is the Temporal workflow type name for deploys.
	WorkflowName string `json:"workflow_name"`
}

// InitDefaults initializes default values for EngineConfig.
func (c *EngineConfig) InitDefaults() {
	if c.WorkflowName == "" {
		c.WorkflowName = DefaultWorkflowName
	}
}

// Validate checks that EngineConfig is valid.
func (c *EngineConfig) Validate() error {
	if c.Worker.Name == "" {
		return ErrWorkerRequired
	}
	if c.Client.Name == "" {
		return ErrClientRequired
	}
	if c.Database.Name == "" {
		return ErrDatabaseRequired
	}
	if c.Host.Name == "" {
		return ErrHostRequired
	}
	return nil
}

// ProviderConfig is the provider binding manifest carried by a
// cloudgraph.provider entry. Manifests are immutable for the process lifetime
// after registration.
type ProviderConfig struct {
	// Meta holds arbitrary metadata associated with this provider entry.
	Meta attrs.Bag `json:"meta"`
	// ProviderType is the unique provider identifier resource specs reference.
	ProviderType string `json:"provider_type"`
	// ActorProcess references the process.lua entry implementing the apply actor.
	ActorProcess registry.ID `json:"actor_process"`
	// Executor references the exec.* entry the actor uses for external commands.
	Executor registry.ID `json:"executor"`
	// ResourceTypes lists the resource types this provider handles.
	ResourceTypes []string `json:"resource_types"`
}

// Validate checks that ProviderConfig is valid.
func (c *ProviderConfig) Validate() error {
	if c.ProviderType == "" {
		return ErrProviderTypeRequired
	}
	if c.ActorProcess.Name == "" {
		return ErrActorProcessRequired
	}
	if len(c.ResourceTypes) == 0 {
		return ErrResourceTypesRequired
	}
	return nil
}
