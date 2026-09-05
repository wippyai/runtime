// SPDX-License-Identifier: MPL-2.0
package socket

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
)

const SocketStreamWait dispatcher.CommandID = 36

func init() { dispatcher.MustRegisterCommands("socket.stream", SocketStreamWait) }

// StreamWaitCmd runs a cancellable buffered I/O continuation off the process worker.
// Run must own its inputs and must never access guest memory.
type StreamWaitCmd struct{ Run func(context.Context) any }

func (*StreamWaitCmd) CmdID() dispatcher.CommandID { return SocketStreamWait }
