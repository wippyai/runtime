// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
)

var inspectorKey = &ctxapi.Key{Name: "process.inspector"}

// HostStats contains summary statistics for a process host.
type HostStats struct {
	ID         registry.ID
	Workers    uint64
	Processes  uint64
	Executed   uint64
	Stolen     uint64
	QueueDepth uint64
}

// Stats contains information about a running process.
type Stats struct {
	Stats     attrs.Attributes
	PID       pid.PID
	Parent    pid.PID
	Host      registry.ID
	Source    registry.ID
	State     string
	ActorID   string
	Steps     uint64
	StartedAt int64
}

// Inspector provides read-only introspection into running hosts and processes.
type Inspector interface {
	ListHosts() []HostStats
	HostProcesses(hostID registry.ID) []Stats
}

// WithInspector attaches an Inspector to the app context.
func WithInspector(ctx context.Context, i Inspector) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(inspectorKey) == nil {
		ac.With(inspectorKey, i)
	}
	return ctx
}

// GetInspector retrieves the Inspector from the app context.
func GetInspector(ctx context.Context) Inspector {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(inspectorKey); val != nil {
		if i, ok := val.(Inspector); ok {
			return i
		}
	}
	return nil
}
