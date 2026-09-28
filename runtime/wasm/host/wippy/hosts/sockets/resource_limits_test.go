// SPDX-License-Identifier: MPL-2.0
package sockets

import (
	"context"
	"testing"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestSocketLimitSharedAcrossTCPAndUDP(t *testing.T) {
	resources := preview2.NewResourceTableWithLimits(8, 1)
	defer resources.Clear()
	tcp, udp := NewTCPCreateSocketHost(resources), NewUDPCreateSocketHost(resources)
	h, err := tcp.CreateTCPSocket(context.Background(), AddressFamilyIPv4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = udp.CreateUDPSocket(context.Background(), AddressFamilyIPv4); err == nil || err.Code != NetworkErrorNewSocketLimit {
		t.Fatalf("UDP limit: %v", err)
	}
	resources.Remove(h)
	if _, err = udp.CreateUDPSocket(context.Background(), AddressFamilyIPv4); err != nil {
		t.Fatalf("capacity not returned: %v", err)
	}
}
