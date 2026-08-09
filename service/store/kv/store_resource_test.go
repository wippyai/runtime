// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/store"
	systemresource "github.com/wippyai/runtime/system/resource"
	"go.uber.org/zap"
)

func TestD10KVResourceStateContract(t *testing.T) {
	s := NewStore(registry.ParseID("test:store"), "resource", nil, nil, zap.NewNop())

	r, err := s.Acquire(context.Background(), s.id, resource.ModeNormal)
	require.NoError(t, err)
	got, err := r.Get()
	require.NoError(t, err)
	require.Same(t, store.Store(s), got)

	r.Release()
	got, err = r.Get()
	require.ErrorIs(t, err, resource.ErrReleased)
	require.Nil(t, got)

	locked, err := s.Acquire(context.Background(), s.id, resource.ModeExclusive)
	require.ErrorIs(t, err, systemresource.ErrLocked)
	require.Nil(t, locked)
}
