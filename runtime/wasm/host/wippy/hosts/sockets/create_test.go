// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"context"
	"testing"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestS15CreateRejectsUnknownFamily(t *testing.T) {
	resources := preview2.NewResourceTable()
	tcpHost := NewTCPCreateSocketHost(resources)
	udpHost := NewUDPCreateSocketHost(resources)
	creators := []struct {
		create func(context.Context, uint8) (uint32, *NetworkError)
		name   string
	}{
		{name: "TCP", create: tcpHost.CreateTCPSocket},
		{name: "UDP", create: udpHost.CreateUDPSocket},
	}

	for _, creator := range creators {
		handle, err := creator.create(context.Background(), 0xff)
		requireNetworkError(t, err, NetworkErrorInvalidArgument)
		if handle != 0 {
			t.Fatalf("%s unknown-family handle = %d, want zero", creator.name, handle)
		}
	}
	probe := resources.Add(&preview2.PollableResource{})
	if probe != 1 {
		t.Fatalf("first resource handle after rejected creates = %d, want 1", probe)
	}
}
