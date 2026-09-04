// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/service/cdc"
)

func TestAdmissionBoundsAggregateReservations(t *testing.T) {
	a := newAdmission(api.SubscriptionLimits{MaxSubscriptions: 2, MaxSnapshots: 1, MaxBytes: 100})
	release, err := a.acquire(60, true)
	require.NoError(t, err)
	_, err = a.acquire(1, true)
	require.ErrorIs(t, err, api.ErrSubscriptionLimit)
	_, err = a.acquire(41, false)
	require.ErrorIs(t, err, api.ErrSubscriptionLimit)
	other, err := a.acquire(40, false)
	require.NoError(t, err)
	_, err = a.acquire(1, false)
	require.ErrorIs(t, err, api.ErrSubscriptionLimit)
	release()
	release()
	other()
	again, err := a.acquire(100, true)
	require.NoError(t, err)
	again()
}

func TestSlotAdmissionReleasesOnClose(t *testing.T) {
	slot := newSourceSlot(registry.NewID("test", "source"), "db.cdc.test", &managedTestSource{})
	slot.admission = newAdmission(api.SubscriptionLimits{MaxSubscriptions: 1, MaxBytes: 100})
	stream, err := slot.Subscribe(context.Background(), api.StreamOptions{MaxBytes: 100})
	require.NoError(t, err)
	_, err = slot.Subscribe(context.Background(), api.StreamOptions{MaxBytes: 1})
	require.ErrorIs(t, err, api.ErrSubscriptionLimit)
	require.Equal(t, 1, slot.Info().Admission.Active)
	stream.Close()
	require.Zero(t, slot.Info().Admission.Active)
	next, err := slot.Subscribe(context.Background(), api.StreamOptions{MaxBytes: 100})
	require.NoError(t, err)
	next.Close()
}
