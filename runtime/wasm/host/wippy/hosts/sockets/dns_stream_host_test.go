// SPDX-License-Identifier: MPL-2.0
package sockets

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	socketapi "github.com/wippyai/runtime/api/socket"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestDNSLiteralRequiresNoAsyncifyOrLookup(t *testing.T) {
	table := preview2.NewResourceTable()
	t.Cleanup(func() { require.NoError(t, table.Close()) })
	network := table.Add(preview2.NewNetworkResource())
	host := NewIPNameLookupHost(table)
	handle, err := host.ResolveAddresses(udpTestContext(), network, "::ffff:192.0.2.9")
	require.Nil(t, err)
	address, err := host.MethodResolveAddressStreamResolveNextAddress(t.Context(), handle)
	require.Nil(t, err)
	require.NotNil(t, address.IPv4)
	require.Nil(t, address.IPv6)
	require.Equal(t, "192.0.2.9", address.String())
	address, err = host.MethodResolveAddressStreamResolveNextAddress(t.Context(), handle)
	require.Nil(t, err)
	require.Nil(t, address)
}

func TestDNSRejectsInvalidNameAndNetworkBeforeDispatch(t *testing.T) {
	table := preview2.NewResourceTable()
	t.Cleanup(func() { require.NoError(t, table.Close()) })
	network := table.Add(preview2.NewNetworkResource())
	host := NewIPNameLookupHost(table)
	handle, err := host.ResolveAddresses(udpTestContext(), network, "a..b")
	require.Zero(t, handle)
	requireNetworkError(t, err, NetworkErrorInvalidArgument)
	handle, err = host.ResolveAddresses(context.Background(), network, "192.0.2.1")
	require.Zero(t, handle)
	requireNetworkError(t, err, NetworkErrorAccessDenied)
	require.Panics(t, func() { host.ResolveAddresses(udpTestContext(), 999, "example.com") })
	wrong := table.Add(&preview2.PollableResource{})
	require.Panics(t, func() { host.ResolveAddresses(udpTestContext(), wrong, "example.com") })
}

func TestDNSLiteralPublicationRespectsHandleLimit(t *testing.T) {
	table := preview2.NewResourceTableWithLimits(1, 1)
	t.Cleanup(func() { require.NoError(t, table.Close()) })
	network := table.Add(preview2.NewNetworkResource())
	handle, err := NewIPNameLookupHost(table).ResolveAddresses(udpTestContext(), network, "192.0.2.1")
	require.Zero(t, handle)
	requireNetworkError(t, err, NetworkErrorOutOfMemory)
}

func TestDNSAckReturnsPendingStreamAndLiveReadiness(t *testing.T) {
	table := preview2.NewResourceTable()
	t.Cleanup(func() { require.NoError(t, table.Close()) })
	host := NewIPNameLookupHost(table)
	op := socketapi.NewPendingOperation()
	stream := preview2.NewPendingResolveAddressStreamResource(op)
	handle := table.Add(stream)
	host.pendingResolve = handle
	returned, err := host.ResolveAddresses(rewindContext(t, &socketapi.StartResult{}), 0, "unused")
	require.Nil(t, err)
	require.Equal(t, handle, returned)
	require.Zero(t, host.pendingResolve)
	address, err := host.MethodResolveAddressStreamResolveNextAddress(t.Context(), handle)
	require.Nil(t, address)
	requireNetworkError(t, err, NetworkErrorWouldBlock)
	subscription := host.MethodResolveAddressStreamSubscribe(t.Context(), handle)
	resource, ok := table.Get(subscription)
	require.True(t, ok)
	pollable, ok := resource.(preview2.NotifyPollable)
	require.True(t, ok)
	require.False(t, pollable.Ready())
	wake := pollable.Notify()
	result, makeErr := socketapi.NewResolvedAddresses([]string{"192.0.2.1", "2001:db8::1", "::ffff:198.51.100.7"})
	require.NoError(t, makeErr)
	op.Complete(result, nil)
	select {
	case <-wake:
	default:
		t.Fatal("lookup completion did not notify")
	}
	require.True(t, pollable.Ready())
	for _, want := range []string{"192.0.2.1", "2001:db8::1", "198.51.100.7"} {
		address, err := host.MethodResolveAddressStreamResolveNextAddress(t.Context(), handle)
		require.Nil(t, err)
		require.Equal(t, want, address.String())
	}
	address, err = host.MethodResolveAddressStreamResolveNextAddress(t.Context(), handle)
	require.Nil(t, err)
	require.Nil(t, address)
	require.True(t, pollable.Ready(), "EOF remains readable")
	table.Remove(subscription)
	require.NotNil(t, host.resolveStream(handle), "subscription borrows stream")
}

func TestDNSRejectedAckDisposesPendingStream(t *testing.T) {
	table := preview2.NewResourceTable()
	t.Cleanup(func() { require.NoError(t, table.Close()) })
	host := NewIPNameLookupHost(table)
	op := socketapi.NewPendingOperation()
	handle := table.Add(preview2.NewPendingResolveAddressStreamResource(op))
	host.pendingResolve = handle
	returned, err := host.ResolveAddresses(rewindContext(t, &socketapi.StartResult{Err: context.Canceled}), 0, "unused")
	require.Zero(t, returned)
	requireNetworkError(t, err, NetworkErrorConnectionAborted)
	_, exists := table.Get(handle)
	require.False(t, exists)
	require.True(t, op.Ready(), "removing stream closes unstarted operation")
	require.Zero(t, host.pendingResolve)
}

func TestDNSInvalidStreamHandlesTrap(t *testing.T) {
	table := preview2.NewResourceTable()
	t.Cleanup(func() { require.NoError(t, table.Close()) })
	host := NewIPNameLookupHost(table)
	wrong := table.Add(preview2.NewNetworkResource())
	for _, handle := range []uint32{0, 999, wrong} {
		require.Panics(t, func() { host.MethodResolveAddressStreamResolveNextAddress(t.Context(), handle) })
		require.Panics(t, func() { host.MethodResolveAddressStreamSubscribe(t.Context(), handle) })
		require.Panics(t, func() { host.ResourceDropResolveAddressStream(t.Context(), handle) })
	}
	require.Equal(t, socketapi.MaxResolveAddresses, preview2.MaxResolveAddresses)
	require.Equal(t, socketapi.MaxResolveAddressBytes, preview2.MaxResolveAddressBytes)
}

func TestDNSStreamDropJoinsActualPendingLookup(t *testing.T) {
	table := preview2.NewResourceTable()
	host := NewIPNameLookupHost(table)
	op := socketapi.NewPendingOperation()
	ctx, started := op.Start(t.Context())
	require.True(t, started)
	handle := table.Add(preview2.NewPendingResolveAddressStreamResource(op))
	cancellationObserved := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { unblock(); require.NoError(t, table.Close()) })
	go func() {
		defer close(exited)
		<-ctx.Done()
		close(cancellationObserved)
		<-release
		op.Complete(nil, ctx.Err())
	}()
	subscription := host.MethodResolveAddressStreamSubscribe(t.Context(), handle)
	resource, ok := table.Get(subscription)
	require.True(t, ok)
	pollable := resource.(preview2.NotifyPollable)
	require.False(t, pollable.Ready())
	notification := pollable.Notify()
	dropped := make(chan struct{})
	go func() { host.ResourceDropResolveAddressStream(t.Context(), handle); close(dropped) }()
	select {
	case <-cancellationObserved:
	case <-time.After(time.Second):
		t.Fatal("stream drop did not cancel lookup")
	}
	select {
	case <-dropped:
		t.Fatal("stream drop returned while lookup cleanup was blocked")
	default:
	}
	unblock()
	select {
	case <-dropped:
	case <-time.After(time.Second):
		t.Fatal("stream drop failed to join")
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("lookup outlived completed drop")
	}
	select {
	case <-notification:
	default:
		t.Fatal("dropped stream notification was lost")
	}
	require.True(t, pollable.Ready())
}
