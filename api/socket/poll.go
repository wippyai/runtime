// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"context"

	"github.com/wippyai/runtime/api/dispatcher"
)

const SocketPollWait dispatcher.CommandID = 35

func init() { dispatcher.MustRegisterCommands("socket.poll", SocketPollWait) }

// PollWaitCmd waits on host readiness signals outside the process worker.
// Wait must return on context cancellation and must not access guest memory.
type PollWaitCmd struct {
	Wait func(context.Context) ([]uint32, error)
}

func (*PollWaitCmd) CmdID() dispatcher.CommandID { return SocketPollWait }
