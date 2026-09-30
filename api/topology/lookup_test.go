// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology/namereg/global"
)

type scopedLocalRegistry struct {
	PIDRegistry
	lookup func(string) (pid.PID, bool)
}

func (r scopedLocalRegistry) LookupLocal(name string) (pid.PID, bool) {
	return r.lookup(name)
}

type scopedGlobalRegistry struct {
	global.Registry
	lookup func(context.Context, string) (global.LookupResult, error)
}

func (r scopedGlobalRegistry) Lookup(ctx context.Context, name string, _ ...global.LookupOption) (global.LookupResult, error) {
	return r.lookup(ctx, name)
}

type scopedEventualRegistry struct {
	EventualRegistry
	lookup func(context.Context, string) (global.LookupResult, error)
}

func (r scopedEventualRegistry) Lookup(ctx context.Context, name string, _ ...global.LookupOption) (global.LookupResult, error) {
	return r.lookup(ctx, name)
}

func bindScopedLookup(ctx context.Context, scope RegistrationMode, local func(string) (pid.PID, bool), remote func(context.Context, string) (global.LookupResult, error)) {
	switch scope {
	case Local:
		WithRegistry(ctx, scopedLocalRegistry{lookup: local})
	case Eventual:
		WithEventualRegistry(ctx, scopedEventualRegistry{lookup: remote})
	case Consistent, Strong:
		global.WithRegistry(ctx, scopedGlobalRegistry{lookup: remote})
	}
}

func TestLookupScopedPID_InvalidAndUnavailable(t *testing.T) {
	for _, scope := range []RegistrationMode{-1, Local, Eventual, Consistent, Strong, 4} {
		p, found, err := LookupScopedPID(context.Background(), "service", scope)
		if scope < Local || scope > Strong {
			require.ErrorIs(t, err, ErrInvalidNameScope)
		} else {
			require.ErrorIs(t, err, ErrNameRegistryUnavailable)
		}
		require.Zero(t, p)
		require.False(t, found)
	}
}

func TestLookupScopedPID_Cancellation(t *testing.T) {
	for _, scope := range []RegistrationMode{Local, Eventual, Consistent, Strong} {
		for _, cancelBefore := range []bool{false, true} {
			ctx, cancel := context.WithCancel(ctxapi.NewRootContext())
			defer cancel()
			calls := 0
			owner := pid.PID{Host: "app", UniqID: "owner"}
			lookup := func(name string) (pid.PID, bool) {
				require.Equal(t, "service", name)
				calls++
				cancel()
				return owner, true
			}
			bindScopedLookup(ctx, scope, lookup, func(_ context.Context, name string) (global.LookupResult, error) {
				p, found := lookup(name)
				return global.LookupResult{PID: p, Found: found}, nil
			})
			if cancelBefore {
				cancel()
			}
			p, found, err := LookupScopedPID(ctx, "service", scope)
			require.ErrorIs(t, err, context.Canceled)
			require.Zero(t, p)
			require.False(t, found)
			if cancelBefore {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
		}
	}
}

func TestLookupScopedPID_DiscardsResultOnBackendError(t *testing.T) {
	backendErr := errors.New("backend failed")
	for _, scope := range []RegistrationMode{Eventual, Consistent, Strong} {
		ctx := ctxapi.NewRootContext()
		bindScopedLookup(ctx, scope, nil, func(context.Context, string) (global.LookupResult, error) {
			return global.LookupResult{PID: pid.PID{Host: "app", UniqID: "owner"}, Found: true}, backendErr
		})
		p, found, err := LookupScopedPID(ctx, "service", scope)
		require.ErrorIs(t, err, backendErr)
		require.Zero(t, p)
		require.False(t, found)
	}
}
