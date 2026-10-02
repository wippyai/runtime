// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
)

func childSpawnOptions() attrs.Bag {
	return attrs.Bag{ProcessParentKey: pid.PID{Host: "h", UniqID: "parent"}}
}

func limitedContext(t *testing.T, slots *ChildSlots) context.Context {
	t.Helper()
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	require.NoError(t, fc.SetMultiple(ChildSlotsPair(slots)))
	return ctx
}

func TestChildSlotResolverReservesUntilRelease(t *testing.T) {
	slots := NewChildSlots(2)
	ctx := limitedContext(t, slots)

	first, err := ChildSlotResolver(ctx, childSpawnOptions())
	require.NoError(t, err)
	require.Len(t, first, 1)
	second, err := ChildSlotResolver(ctx, childSpawnOptions())
	require.NoError(t, err)
	require.Equal(t, 2, slots.InUse())

	_, err = ChildSlotResolver(ctx, childSpawnOptions())
	require.ErrorIs(t, err, ErrChildLimitExceeded)

	// A child exits: its frame closes the slot.
	require.NoError(t, first[0].Value.(ctxapi.FrameAttachment).Close())
	require.Equal(t, 1, slots.InUse())
	_, err = ChildSlotResolver(ctx, childSpawnOptions())
	require.NoError(t, err)

	// A failed spawn rolls its slot back; release is idempotent.
	attachment := second[0].Value.(ctxapi.FrameAttachment)
	require.NoError(t, attachment.Rollback())
	require.NoError(t, attachment.Close())
	require.Equal(t, 1, slots.InUse())
}

func TestChildSlotResolverIgnoresUnlimitedAndNonSpawnFrames(t *testing.T) {
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)
	pairs, err := ChildSlotResolver(ctx, childSpawnOptions())
	require.NoError(t, err)
	require.Empty(t, pairs, "processes without a limit reserve nothing")

	slots := NewChildSlots(1)
	limited := limitedContext(t, slots)
	pairs, err = ChildSlotResolver(limited, attrs.Bag{})
	require.NoError(t, err)
	require.Empty(t, pairs, "frames that are not child processes reserve nothing")
	require.Zero(t, slots.InUse())
}

func TestChildSlotsAreNotInherited(t *testing.T) {
	slots := NewChildSlots(1)
	forked, fc := ctxapi.ForkFrameContext(limitedContext(t, slots))
	defer ctxapi.ReleaseFrameContext(fc)
	pairs, err := ChildSlotResolver(forked, childSpawnOptions())
	require.NoError(t, err)
	require.Empty(t, pairs)
}
