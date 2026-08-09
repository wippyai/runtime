// SPDX-License-Identifier: MPL-2.0

package resource

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	apiresource "github.com/wippyai/runtime/api/resource"
)

type failingBoundaryRegistry struct {
	getErr   error
	acquires atomic.Int64
	releases atomic.Int64
}

func (r *failingBoundaryRegistry) Acquire(context.Context, registry.ID, apiresource.AccessMode) (apiresource.Resource[any], error) {
	r.acquires.Add(1)
	return &failingBoundaryResource{err: r.getErr, releases: &r.releases}, nil
}
func (*failingBoundaryRegistry) List() ([]registry.ID, error) { return nil, nil }
func (*failingBoundaryRegistry) Exists(registry.ID) bool      { return true }

type failingBoundaryResource struct {
	err      error
	releases *atomic.Int64
	released atomic.Bool
}

func (r *failingBoundaryResource) Get() (any, error) { return nil, r.err }
func (r *failingBoundaryResource) Release() {
	if r.released.CompareAndSwap(false, true) {
		r.releases.Add(1)
	}
}

func contextWithBoundaryStore(t *testing.T) (context.Context, *Store) {
	t.Helper()
	ctx, frame := ctxapi.OpenFrameContext(context.Background())
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(frame) })
	store := NewStore()
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, SetStore(ctx, store))
	return ctx, store
}

func TestA06DirectAcquireReleasesOnGetFailure(t *testing.T) {
	getErr := errors.New("direct get failure")
	reg := &failingBoundaryRegistry{getErr: getErr}

	res, value, err := AcquireRegistryResource(context.Background(), reg, registry.ParseID("boundary:direct"), apiresource.ModeNormal)

	assert.Nil(t, res)
	assert.Nil(t, value)
	assert.Same(t, getErr, err)
	assert.Equal(t, int64(1), reg.acquires.Load())
	assert.Equal(t, int64(1), reg.releases.Load())
}

func TestA07StoreAcquireReleasesOnGetFailure(t *testing.T) {
	ctx, _ := contextWithBoundaryStore(t)
	getErr := errors.New("store get failure")
	reg := &failingBoundaryRegistry{getErr: getErr}

	res, value, err := AcquireRegistryResource(ctx, reg, registry.ParseID("boundary:stored"), apiresource.ModeNormal)

	assert.Nil(t, res)
	assert.Nil(t, value)
	assert.Same(t, getErr, err)
	assert.Equal(t, int64(1), reg.acquires.Load())
	assert.Equal(t, int64(1), reg.releases.Load())
}

func TestA08CanceledAcquireAvoidsRegistry(t *testing.T) {
	ctx, _ := contextWithBoundaryStore(t)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	reg := &failingBoundaryRegistry{getErr: errors.New("must not be observed")}

	res, value, err := AcquireRegistryResource(ctx, reg, registry.ParseID("boundary:canceled"), apiresource.ModeNormal)

	assert.Nil(t, res)
	assert.Nil(t, value)
	assert.Same(t, context.Canceled, err)
	assert.Zero(t, reg.acquires.Load())
}

func TestA09ClosedStoreAvoidsRegistry(t *testing.T) {
	ctx, store := contextWithBoundaryStore(t)
	require.NoError(t, store.Close())
	reg := &failingBoundaryRegistry{getErr: errors.New("must not be observed")}

	res, value, err := AcquireRegistryResource(ctx, reg, registry.ParseID("boundary:closed"), apiresource.ModeNormal)

	assert.Nil(t, res)
	assert.Nil(t, value)
	assert.ErrorIs(t, err, apiresource.ErrReleased)
	assert.Zero(t, reg.acquires.Load())
}

func TestA11NilLeaseIsReleasedSafe(t *testing.T) {
	var lease *Lease

	value, err := lease.Get()
	assert.Nil(t, value)
	assert.ErrorIs(t, err, apiresource.ErrReleased)
	assert.NotPanics(t, lease.Release)
}
