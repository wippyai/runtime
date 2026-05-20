// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/globalreg"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
)

// ErrCouldNotResolve is returned by ResolveDestination when no resolver
// recognizes the destination string.
var ErrCouldNotResolve = errors.New("could not resolve destination")

// ResolvedDestination holds the outcome of resolving a send-target string.
// GlobalName and FenceToken are populated only when the destination resolved
// through the strongly consistent global registry; raw PIDs, eventual
// names and local names leave both fields zero.
type ResolvedDestination struct {
	PID        pidapi.PID
	GlobalName string
	FenceToken uint64
}

// ResolveDestination converts a raw PID string or a registered name into
// the addressable PID and (for strongly consistent global names) the fence
// token that must accompany outgoing packages so the receiver can reject
// stale references after re-registration.
//
// Resolution order is:
//  1. raw PID parse
//  2. globalreg (fence-bearing)
//  3. eventualreg (no fence)
//  4. local PIDRegistry (no fence)
//
// Registries are read from ctx via globalreg.GetRegistry,
// topology.GetEventualRegistry and topology.GetRegistry. A nil registry at
// any layer is skipped silently — callers that need a layer to be present
// must enforce that themselves.
func ResolveDestination(ctx context.Context, dest string) (ResolvedDestination, error) {
	if p, err := pidapi.ParsePID(dest); err == nil {
		return ResolvedDestination{PID: p}, nil
	}

	if gr := globalreg.GetRegistry(ctx); gr != nil {
		result, err := gr.Lookup(ctx, dest, globalreg.WithFence())
		if err == nil && result.Found {
			return ResolvedDestination{
				PID:        result.PID,
				GlobalName: dest,
				FenceToken: result.FenceToken,
			}, nil
		}
	}

	if er := topology.GetEventualRegistry(ctx); er != nil {
		result, err := er.Lookup(ctx, dest)
		if err == nil && result.Found {
			return ResolvedDestination{PID: result.PID}, nil
		}
	}

	if pr := topology.GetRegistry(ctx); pr != nil {
		if p, ok := pr.Lookup(dest); ok {
			return ResolvedDestination{PID: p}, nil
		}
	}

	return ResolvedDestination{}, ErrCouldNotResolve
}
