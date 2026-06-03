// SPDX-License-Identifier: MPL-2.0

// Package kv provides configuration for the store.kv.* store kinds.
package kv

import (
	"regexp"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Store kind constants.
const (
	// KindRaft is a raft-replicated, fs-durable kv store.
	KindRaft registry.Kind = "store.kv.raft"
	// KindCRDT is a gossip-CRDT kv store with optional fs durability.
	KindCRDT registry.Kind = "store.kv.crdt"
)

// namespacePattern constrains an app namespace so it can never collide with the
// reserved _sys system namespace (a leading underscore is excluded by the
// required leading [a-z]). System coordination lives under _sys:* keys.
var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

// ErrInvalidNamespace is returned for a missing or malformed namespace.
var ErrInvalidNamespace = apierror.New(apierror.Invalid,
	"store.kv namespace must match ^[a-z][a-z0-9._-]*$").WithRetryable(apierror.False)

// RaftConfig configures a store.kv.raft entry. Namespace isolates this store's
// keys within the shared node-wide kv state.
type RaftConfig struct {
	Namespace string                     `json:"namespace"`
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
}

// Validate checks the configuration.
func (c *RaftConfig) Validate() error {
	if !namespacePattern.MatchString(c.Namespace) {
		return ErrInvalidNamespace
	}
	return nil
}

// InitDefaults fills lifecycle defaults.
func (c *RaftConfig) InitDefaults() {
	c.Lifecycle.InitDefaults()
}

// CRDTConfig configures a store.kv.crdt entry.
type CRDTConfig struct {
	Namespace string                     `json:"namespace"`
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
	Durable   bool                       `json:"durable"`
}

// Validate checks the configuration.
func (c *CRDTConfig) Validate() error {
	if !namespacePattern.MatchString(c.Namespace) {
		return ErrInvalidNamespace
	}
	return nil
}

// InitDefaults fills lifecycle defaults.
func (c *CRDTConfig) InitDefaults() {
	c.Lifecycle.InitDefaults()
}
