// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"context"
	"net"
	"testing"

	socketapi "github.com/wippyai/runtime/api/socket"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestS05ResolveRejectsWrongAsyncType(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewIPNameLookupHost(resources)
	left, right := net.Pipe()
	carried := &closeCountingConn{Conn: left}
	t.Cleanup(func() { _ = right.Close() })
	ctx := rewindContext(t, &socketapi.ConnectResult{Conn: carried})

	handle, err := host.ResolveAddresses(ctx, 0, "example.invalid")
	requireNetworkError(t, err, NetworkErrorInvalidArgument)
	if handle != 0 {
		t.Fatalf("resolve stream handle = %d, want zero", handle)
	}
	if carried.closes.Load() != 1 {
		t.Fatalf("unadopted connection close count = %d, want 1", carried.closes.Load())
	}
	probe := resources.Add(&preview2.PollableResource{})
	if probe != 1 {
		t.Fatalf("first resource handle after rejected resolve = %d, want 1", probe)
	}
}

func TestS16ResolveStreamSingleUse(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewIPNameLookupHost(resources)
	addresses := []string{"192.0.2.1", "2001:db8::1", "198.51.100.7"}
	handle := resources.Add(preview2.NewResolveAddressStreamResource(addresses))

	for index, want := range addresses {
		address, err := host.MethodResolveAddressStreamResolveNextAddress(context.Background(), handle)
		if err != nil || address == nil || address.Address != want || address.Port != 0 {
			t.Fatalf("address %d = %#v, error = %v, want %q", index, address, err, want)
		}
	}
	address, err := host.MethodResolveAddressStreamResolveNextAddress(context.Background(), handle)
	if err != nil || address != nil {
		t.Fatalf("exhausted stream = %#v, error = %v, want clean exhaustion", address, err)
	}
	address, err = host.MethodResolveAddressStreamResolveNextAddress(context.Background(), handle)
	if err != nil || address != nil {
		t.Fatalf("re-read exhausted stream = %#v, error = %v, want clean exhaustion", address, err)
	}

	host.ResourceDropResolveAddressStream(context.Background(), handle)
	address, err = host.MethodResolveAddressStreamResolveNextAddress(context.Background(), handle)
	requireNetworkError(t, err, NetworkErrorInvalidArgument)
	if address != nil {
		t.Fatalf("post-drop address = %#v, want nil", address)
	}
}
