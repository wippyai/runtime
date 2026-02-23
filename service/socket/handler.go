// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"context"
	"fmt"
	"net"

	"github.com/wippyai/runtime/api/dispatcher"
	netapi "github.com/wippyai/runtime/api/net"
	socketapi "github.com/wippyai/runtime/api/socket"
)

// Dispatcher handles socket commands through the dispatcher system.
type Dispatcher struct {
	netSvc netapi.Service
}

// NewDispatcher creates a socket dispatcher backed by a network service.
func NewDispatcher(netSvc netapi.Service) *Dispatcher {
	return &Dispatcher{netSvc: netSvc}
}

// RegisterAll registers all socket command handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(socketapi.SocketConnect, dispatcher.HandlerFunc(d.handleConnect))
	register(socketapi.SocketListen, dispatcher.HandlerFunc(d.handleListen))
	register(socketapi.SocketAccept, dispatcher.HandlerFunc(d.handleAccept))
	register(socketapi.SocketBind, dispatcher.HandlerFunc(d.handleBind))
	register(socketapi.SocketResolve, dispatcher.HandlerFunc(d.handleResolve))
}

func (d *Dispatcher) handleConnect(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*socketapi.ConnectCmd)
	go func() {
		conn, err := d.netSvc.DialContext(ctx, c.Network, c.Address)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, &socketapi.ConnectResult{Conn: conn, Err: err}, nil)
		} else if conn != nil {
			_ = conn.Close()
		}
	}()
	return nil
}

func (d *Dispatcher) handleListen(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*socketapi.ListenCmd)
	go func() {
		listener, err := d.netSvc.Listen(ctx, c.Network, c.Address)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, &socketapi.ListenResult{Listener: listener, Err: err}, nil)
		} else if listener != nil {
			_ = listener.Close()
		}
	}()
	return nil
}

func (d *Dispatcher) handleAccept(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*socketapi.AcceptCmd)
	go func() {
		conn, err := c.Listener.Accept()
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, &socketapi.AcceptResult{Conn: conn, Err: err}, nil)
		} else if conn != nil {
			_ = conn.Close()
		}
	}()
	return nil
}

func (d *Dispatcher) handleBind(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*socketapi.BindCmd)
	go func() {
		pc, err := d.netSvc.ListenPacket(ctx, c.Network, c.Address)
		if ctx.Err() != nil {
			if pc != nil {
				_ = pc.Close()
			}
			return
		}
		if err != nil {
			receiver.CompleteYield(tag, &socketapi.BindResult{Err: err}, nil)
			return
		}
		udpConn, ok := pc.(*net.UDPConn)
		if !ok {
			_ = pc.Close()
			receiver.CompleteYield(tag, &socketapi.BindResult{Err: fmt.Errorf("ListenPacket returned %T, expected *net.UDPConn", pc)}, nil)
			return
		}
		receiver.CompleteYield(tag, &socketapi.BindResult{Conn: udpConn}, nil)
	}()
	return nil
}

func (d *Dispatcher) handleResolve(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*socketapi.ResolveCmd)
	go func() {
		addrs, err := d.netSvc.LookupHost(ctx, c.Host)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, &socketapi.ResolveResult{Addresses: addrs, Err: err}, nil)
		}
	}()
	return nil
}
