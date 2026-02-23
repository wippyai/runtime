// SPDX-License-Identifier: MPL-2.0

// Package workflow provides support for deterministic execution in workflow contexts.
// When deterministic mode is enabled, modules that perform non-deterministic operations
// (UUID generation, random numbers, time) yield their operations to be handled
// by an external dispatcher that can record and replay results.
package workflow

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var (
	deterministicKey = &ctxapi.Key{Name: "workflow.deterministic"}
	infoKey          = &ctxapi.Key{Name: "workflow.info"}
)

// Info contains workflow execution information.
type Info struct {
	WorkflowID    string
	RunID         string
	WorkflowType  string
	TaskQueue     string
	Namespace     string
	Attempt       int
	HistoryLength int
	HistorySize   int
}

// InfoProvider is implemented by workflow executors to provide runtime info.
type InfoProvider interface {
	GetWorkflowInfo() Info
}

// SetDeterministic marks the frame context as requiring deterministic execution.
// Non-deterministic operations will yield to the dispatcher for recording/replay.
func SetDeterministic(ctx context.Context) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(deterministicKey, true)
}

// IsDeterministic returns true if the context requires deterministic execution.
func IsDeterministic(ctx context.Context) bool {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return false
	}
	if val, ok := fc.Get(deterministicKey); ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}

// SetInfoProvider sets the workflow info provider on the context.
func SetInfoProvider(ctx context.Context, provider InfoProvider) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(infoKey, provider)
}

// GetInfo returns the workflow info from context, or nil if not available.
func GetInfo(ctx context.Context) *Info {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(infoKey); ok {
		if provider, ok := val.(InfoProvider); ok {
			info := provider.GetWorkflowInfo()
			return &info
		}
	}
	return nil
}
