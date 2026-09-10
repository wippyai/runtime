package sockets

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestW1TCPHostBufferConnectAdmission(t *testing.T) {
	for _, tc := range []struct {
		name    string
		bytes   uint64
		handles int
		ok      bool
	}{
		{"deny-first", 65535, 8, false}, {"deny-second", 65536, 8, false},
		{"publish-input-failure", 131072, 1, false}, {"publish-output-failure", 131072, 2, false},
		{"admitted", 131072, 8, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buffers := preview2.NewHostBufferBudget(tc.bytes)
			table := preview2.NewResourceTableWithBudgets(tc.handles, preview2.NewSocketBudget(2), buffers)
			defer table.Close()
			socket := preview2.NewTCPSocketResource(AddressFamilyIPv4)
			socket.SetState(preview2.TCPStateConnectInProgress)
			handle := table.Add(socket)
			left, right := net.Pipe()
			defer right.Close()
			conn := &closeCountingConn{Conn: left}
			attachCompletedTCPOperation(t, socket, conn, nil)
			streams, err := NewTCPHost(table).MethodTCPSocketFinishConnect(t.Context(), handle)
			if tc.ok {
				if err != nil || streams == nil {
					t.Fatalf("connect: %v / %v", streams, err)
				}
				if buffers.Usage().Used != 131072 {
					t.Fatalf("ring charge: %+v", buffers.Usage())
				}
			} else {
				requireNetworkError(t, err, NetworkErrorOutOfMemory)
				if streams != nil || socket.State() != preview2.TCPStateClosed {
					t.Fatal("failed connection remains usable")
				}
				if buffers.Usage().Used != 0 || conn.closes.Load() != 1 {
					t.Fatal("failed connection retained ring charge/connection")
				}
			}
			table.Close()
			if buffers.Usage().Used != 0 || table.SocketBudget().Used() != 0 {
				t.Fatal("table close retained quota")
			}
		})
	}
}

func TestW1TCPHostBufferAcceptAdmission(t *testing.T) {
	for _, tc := range []struct {
		name    string
		bytes   uint64
		handles int
		ok      bool
	}{
		{"deny-first", 65535, 8, false}, {"deny-second", 65536, 8, false},
		{"publish-child-failure", 131072, 1, false}, {"publish-input-failure", 131072, 2, false},
		{"publish-output-failure", 131072, 3, false}, {"admitted", 131072, 8, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buffers := preview2.NewHostBufferBudget(tc.bytes)
			table := preview2.NewResourceTableWithBudgets(tc.handles, preview2.NewSocketBudget(3), buffers)
			defer table.Close()
			host := NewTCPHost(table)
			listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			parent := preview2.NewTCPSocketResource(AddressFamilyIPv4)
			parent.SetState(preview2.TCPStateListenInProgress)
			attachCompletedTCPOperation(t, parent, listener, nil)
			handle := table.Add(parent)
			if err := host.MethodTCPSocketFinishListen(t.Context(), handle); err != nil {
				t.Fatal(err)
			}
			client, err := (&net.Dialer{Timeout: time.Second}).DialContext(t.Context(), "tcp4", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			parent.AcceptQueue().Block(ctx)
			accepted, netErr := host.MethodTCPSocketAccept(t.Context(), handle)
			if tc.ok {
				if netErr != nil || accepted == nil {
					t.Fatalf("accept: %v / %v", accepted, netErr)
				}
				if buffers.Usage().Used != 131072 {
					t.Fatalf("ring charge: %+v", buffers.Usage())
				}
			} else {
				requireNetworkError(t, netErr, NetworkErrorOutOfMemory)
				if accepted != nil || buffers.Usage().Used != 0 {
					t.Fatal("failed accept published tuple or retained ring charge")
				}
			}
			table.Close()
			if buffers.Usage().Used != 0 || table.SocketBudget().Used() != 0 {
				t.Fatal("table close retained quota")
			}
		})
	}
}
