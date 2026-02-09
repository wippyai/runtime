package socket

import (
	"net"

	"github.com/wippyai/runtime/api/dispatcher"
)

func init() {
	dispatcher.MustRegisterCommands("socket",
		SocketConnect, SocketListen, SocketAccept, SocketBind, SocketResolve)
}

// Command IDs for socket operations.
// Range 30-39 is reserved for socket commands.
const (
	SocketConnect dispatcher.CommandID = 30
	SocketListen  dispatcher.CommandID = 31
	SocketAccept  dispatcher.CommandID = 32
	SocketBind    dispatcher.CommandID = 33
	SocketResolve dispatcher.CommandID = 34
)

// ConnectCmd requests a TCP connection to a remote address.
type ConnectCmd struct {
	Network string
	Address string
}

func (c *ConnectCmd) CmdID() dispatcher.CommandID { return SocketConnect }

// ConnectResult holds the result of a connect operation.
type ConnectResult struct {
	Conn net.Conn
	Err  error
}

// ListenCmd requests a TCP listener on an address.
type ListenCmd struct {
	Network string
	Address string
}

func (c *ListenCmd) CmdID() dispatcher.CommandID { return SocketListen }

// ListenResult holds the result of a listen operation.
type ListenResult struct {
	Listener net.Listener
	Err      error
}

// AcceptCmd requests accepting a connection from a listener.
type AcceptCmd struct {
	Listener net.Listener
}

func (c *AcceptCmd) CmdID() dispatcher.CommandID { return SocketAccept }

// AcceptResult holds the result of an accept operation.
type AcceptResult struct {
	Conn net.Conn
	Err  error
}

// BindCmd requests binding a UDP socket to an address.
type BindCmd struct {
	Network string
	Address string
}

func (c *BindCmd) CmdID() dispatcher.CommandID { return SocketBind }

// BindResult holds the result of a bind operation.
type BindResult struct {
	Conn *net.UDPConn
	Err  error
}

// ResolveCmd requests DNS resolution of a hostname.
type ResolveCmd struct {
	Host string
}

func (c *ResolveCmd) CmdID() dispatcher.CommandID { return SocketResolve }

// ResolveResult holds the result of a resolve operation.
type ResolveResult struct {
	Addresses []string
	Err       error
}
