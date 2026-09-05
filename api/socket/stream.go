// SPDX-License-Identifier: MPL-2.0
package socket

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
)

const SocketStreamWait dispatcher.CommandID = 36

func init() { dispatcher.MustRegisterCommands("socket.stream", SocketStreamWait) }

// StreamWaitCmd runs a cancellable buffered I/O continuation off the process worker.
// Run must own its inputs and must never access guest memory.
// Deadline is the absolute bound for this logical host operation; a zero value
// keeps the caller context. The service must not reset it across stages.
type StreamWaitCmd struct {
	Run      func(context.Context) any
	Deadline time.Time
}

func (*StreamWaitCmd) CmdID() dispatcher.CommandID { return SocketStreamWait }
