// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

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
	register(socketapi.SocketStartBind, dispatcher.HandlerFunc(d.handleStartBind))
	register(socketapi.SocketResolve, dispatcher.HandlerFunc(d.handleResolve))
	register(socketapi.SocketPollWait, dispatcher.HandlerFunc(d.handlePollWait))
	register(socketapi.SocketStreamWait, dispatcher.HandlerFunc(d.handleStreamWait))
	register(socketapi.SocketStartConnect, dispatcher.HandlerFunc(d.handleStartConnect))
	register(socketapi.SocketStartListen, dispatcher.HandlerFunc(d.handleStartListen))
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
		if c == nil || c.Listener == nil {
			if ctx.Err() == nil {
				receiver.CompleteYield(tag, &socketapi.AcceptResult{Err: fmt.Errorf("nil listener")}, nil)
			}
			return
		}

		cancelDone := make(chan struct{})
		stop := context.AfterFunc(ctx, func() {
			defer close(cancelDone)
			_ = c.Listener.Close()
		})

		conn, err := c.Listener.Accept()
		if stop() {
			if ctx.Err() == nil {
				receiver.CompleteYield(tag, &socketapi.AcceptResult{Conn: conn, Err: err}, nil)
				return
			}
		} else {
			<-cancelDone
		}

		if conn != nil {
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
	c, ok := cmd.(*socketapi.ResolveCmd)
	if !ok || c == nil {
		receiver.CompleteYield(tag, &socketapi.StartResult{Err: socketapi.ErrNilOperation}, nil)
		return nil
	}
	if c.Operation == nil {
		go func() {
			addrs, err := d.netSvc.LookupHost(ctx, c.Host)
			if ctx.Err() == nil {
				receiver.CompleteYield(tag, &socketapi.ResolveResult{Addresses: addrs, Err: err}, nil)
			}
		}()
		return nil
	}
	return d.startJob(ctx, c.Operation, c.Timeout, tag, receiver, func(opCtx context.Context) (io.Closer, error) {
		addrs, err := d.netSvc.LookupHost(opCtx, c.Host)
		if err != nil {
			return nil, err
		}
		if len(addrs) == 0 {
			return nil, &net.DNSError{Err: "no suitable addresses", Name: c.Host, IsNotFound: true}
		}
		return socketapi.NewResolvedAddresses(addrs)
	})
}

func (d *Dispatcher) handleStartConnect(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c, ok := cmd.(*socketapi.StartConnectCmd)
	if !ok || c == nil {
		receiver.CompleteYield(tag, &socketapi.StartResult{Err: socketapi.ErrNilOperation}, nil)
		return nil
	}
	return d.startJob(ctx, c.Operation, c.Timeout, tag, receiver, func(opCtx context.Context) (io.Closer, error) {
		return d.netSvc.DialContext(opCtx, c.Network, c.Address)
	})
}

func (d *Dispatcher) handleStartListen(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c, ok := cmd.(*socketapi.StartListenCmd)
	if !ok || c == nil {
		receiver.CompleteYield(tag, &socketapi.StartResult{Err: socketapi.ErrNilOperation}, nil)
		return nil
	}
	return d.startJob(ctx, c.Operation, c.Timeout, tag, receiver, func(opCtx context.Context) (io.Closer, error) {
		return d.netSvc.Listen(opCtx, c.Network, c.Address)
	})
}

func (d *Dispatcher) handleStartBind(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c, ok := cmd.(*socketapi.StartBindCmd)
	if !ok || c == nil {
		receiver.CompleteYield(tag, &socketapi.StartResult{Err: socketapi.ErrNilOperation}, nil)
		return nil
	}
	return d.startJob(ctx, c.Operation, c.Timeout, tag, receiver, func(opCtx context.Context) (io.Closer, error) {
		conn, err := d.netSvc.ListenPacket(opCtx, c.Network, c.Address)
		if udp, ok := conn.(*net.UDPConn); ok && udp == nil {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("%w: ListenPacket returned a nil UDP connection", netapi.ErrNotSupported)
		}
		if err != nil {
			return conn, err
		}
		if _, ok := conn.(*net.UDPConn); !ok {
			return conn, fmt.Errorf("%w: ListenPacket returned %T, expected *net.UDPConn", netapi.ErrNotSupported, conn)
		}
		return conn, nil
	})
}

func (d *Dispatcher) startJob(ctx context.Context, op *socketapi.PendingOperation, timeout time.Duration, tag uint64, receiver dispatcher.ResultReceiver, run func(context.Context) (io.Closer, error)) error {
	if op == nil {
		receiver.CompleteYield(tag, &socketapi.StartResult{Err: socketapi.ErrNilOperation}, nil)
		return nil
	}
	if timeout < 0 {
		receiver.CompleteYield(tag, &socketapi.StartResult{Err: socketapi.ErrInvalidTimeout}, nil)
		return nil
	}
	opCtx, ok := op.Start(ctx)
	if !ok {
		receiver.CompleteYield(tag, &socketapi.StartResult{Err: socketapi.ErrAlreadyStarted}, nil)
		return nil
	}
	jobCtx := opCtx
	cancel := context.CancelFunc(func() {})
	if timeout > 0 {
		jobCtx, cancel = context.WithTimeout(opCtx, timeout)
	}
	go func() {
		defer cancel()
		value, err := run(jobCtx)
		if jobCtx.Err() != nil {
			err = jobCtx.Err()
		}
		op.Complete(value, err)
	}()
	receiver.CompleteYield(tag, &socketapi.StartResult{}, nil)
	return nil
}
