// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"

	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/api/topology/namereg/global"
)

// ErrCouldNotResolve is returned by ResolveDestination when no resolver
// recognizes the destination string.
var ErrCouldNotResolve = errors.New("could not resolve destination")

// ResolvedDestination holds the outcome of resolving a send-target string.
type ResolvedDestination struct {
	PID pidapi.PID
}

// ResolveDestination converts a raw PID string or a registered name into
// the addressable PID.
//
// Resolution order is:
//  1. raw PID parse
//  2. globalreg
//  3. eventualreg
//  4. local PIDRegistry
//
// Registries are read from ctx via global.GetRegistry,
// topology.GetEventualRegistry and topology.GetRegistry. A nil registry at
// any layer is skipped silently — callers that need a layer to be present
// must enforce that themselves. An unavailable higher scope does not hide an
// available lower-scope binding. If none resolves, return the first lookup
// error rather than treating the name as absent. Caller cancellation wins.
func ResolveDestination(ctx context.Context, dest string) (ResolvedDestination, error) {
	if err := ctx.Err(); err != nil {
		return ResolvedDestination{}, err
	}
	if p, err := pidapi.ParsePID(dest); err == nil {
		return ResolvedDestination{PID: p}, nil
	}
	var firstErr error

	if gr := global.GetRegistry(ctx); gr != nil {
		result, err := gr.Lookup(ctx, dest)
		if err != nil {
			if ctx.Err() != nil {
				return ResolvedDestination{}, ctx.Err()
			}
			firstErr = err
		} else if result.Found {
			if ctx.Err() != nil {
				return ResolvedDestination{}, ctx.Err()
			}
			return ResolvedDestination{PID: result.PID}, nil
		}
	}

	if er := topology.GetEventualRegistry(ctx); er != nil {
		result, err := er.Lookup(ctx, dest)
		if err != nil {
			if ctx.Err() != nil {
				return ResolvedDestination{}, ctx.Err()
			}
			if firstErr == nil {
				firstErr = err
			}
		} else if result.Found {
			if ctx.Err() != nil {
				return ResolvedDestination{}, ctx.Err()
			}
			return ResolvedDestination{PID: result.PID}, nil
		}
	}

	if err := ctx.Err(); err != nil {
		return ResolvedDestination{}, err
	}
	if pr := topology.GetRegistry(ctx); pr != nil {
		p, found, err := topology.LookupPID(ctx, pr, dest)
		if err != nil {
			if ctx.Err() != nil {
				return ResolvedDestination{}, ctx.Err()
			}
			if firstErr == nil {
				firstErr = err
			}
		}
		if found {
			if ctx.Err() != nil {
				return ResolvedDestination{}, ctx.Err()
			}
			return ResolvedDestination{PID: p}, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return ResolvedDestination{}, err
	}
	if firstErr != nil {
		return ResolvedDestination{}, firstErr
	}

	return ResolvedDestination{}, ErrCouldNotResolve
}
