// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

type unavailableGlobalLookup struct {
	*fakeGlobalRegistry
	failure error
}

func (r *unavailableGlobalLookup) Lookup(context.Context, string, ...globalapi.LookupOption) (globalapi.LookupResult, error) {
	return globalapi.LookupResult{}, r.failure
}

func TestPIDRegistryLegacyLookupDoesNotShadowUnavailableGlobal(t *testing.T) {
	reg := NewPIDRegistry()
	shadow := pid.PID{Host: "h", UniqID: "shadow"}
	_, err := reg.Register("svc", shadow)
	require.NoError(t, err)
	reg.SetGlobalRegistry(&unavailableGlobalLookup{fakeGlobalRegistry: &fakeGlobalRegistry{}, failure: errors.New("unavailable")})
	_, found := reg.Lookup("svc")
	require.False(t, found, "legacy facade cannot expose an error but must not choose a weaker-scope owner")
}

type waitingGlobalLookup struct {
	*fakeGlobalRegistry
	entered chan struct{}
}

func (r *waitingGlobalLookup) Lookup(ctx context.Context, _ string, _ ...globalapi.LookupOption) (globalapi.LookupResult, error) {
	close(r.entered)
	<-ctx.Done()
	return globalapi.LookupResult{}, ctx.Err()
}

func TestPIDRegistryParentLookupPreservesCancellation(t *testing.T) {
	global := &waitingGlobalLookup{fakeGlobalRegistry: &fakeGlobalRegistry{}, entered: make(chan struct{})}
	parent := NewPIDRegistry(WithGlobalRegistry(global))
	child := NewPIDRegistry(WithParent(parent))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, _, err := child.LookupContext(ctx, "svc"); done <- err }()
	select {
	case <-global.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("parent read not reached")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("parent read ignored cancellation")
	}
}

func TestPIDRegistryContextPreservesParentFailure(t *testing.T) {
	failure := errors.New("parent unavailable")
	parent := NewPIDRegistry(WithGlobalRegistry(&unavailableGlobalLookup{fakeGlobalRegistry: &fakeGlobalRegistry{}, failure: failure}))
	child := NewPIDRegistry(WithParent(parent))
	_, found, err := child.LookupContext(context.Background(), "svc")
	require.ErrorIs(t, err, failure)
	require.False(t, found)
}

type unavailableEventualLookup struct {
	*fakeEventualRegistry
	failure error
}

func (r *unavailableEventualLookup) Lookup(context.Context, string, ...globalapi.LookupOption) (globalapi.LookupResult, error) {
	return globalapi.LookupResult{}, r.failure
}

func TestPIDRegistryRegistrationRejectsUnknownCrossScopeOwnership(t *testing.T) {
	for _, scope := range []string{"global", "eventual"} {
		t.Run(scope, func(t *testing.T) {
			reg := NewPIDRegistry()
			owner := pid.PID{Host: "h", UniqID: "owner"}
			_, err := reg.Register("existing", owner)
			require.NoError(t, err)
			failure := errors.New("ownership lookup unavailable")
			if scope == "global" {
				reg.SetGlobalRegistry(&unavailableGlobalLookup{fakeGlobalRegistry: &fakeGlobalRegistry{}, failure: failure})
			} else {
				reg.SetEventualRegistry(&unavailableEventualLookup{fakeEventualRegistry: &fakeEventualRegistry{}, failure: failure})
			}
			_, err = reg.Register("svc", owner)
			require.ErrorIs(t, err, failure)
			_, found := reg.LookupLocal("svc")
			require.False(t, found, "failed ownership check must not create a local binding")
			_, err = reg.Register("existing", owner)
			require.ErrorIs(t, err, failure)
			current, found := reg.LookupLocal("existing")
			require.True(t, found)
			require.True(t, current.Equal(owner), "lookup failure must preserve existing bindings")
		})
	}
}
