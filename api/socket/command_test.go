// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/dispatcher"
)

func TestCommandIDs(t *testing.T) {
	assert.Equal(t, dispatcher.CommandID(30), SocketConnect)
	assert.Equal(t, dispatcher.CommandID(31), SocketListen)
	assert.Equal(t, dispatcher.CommandID(32), SocketAccept)
	assert.Equal(t, dispatcher.CommandID(33), SocketBind)
	assert.Equal(t, dispatcher.CommandID(34), SocketResolve)
	assert.Equal(t, dispatcher.CommandID(35), SocketPollWait)
	assert.Equal(t, dispatcher.CommandID(36), SocketStreamWait)
	assert.Equal(t, dispatcher.CommandID(37), SocketStartConnect)
	assert.Equal(t, dispatcher.CommandID(38), SocketStartListen)
}

func TestStartCommandIDs(t *testing.T) {
	connect := &StartConnectCmd{Network: "tcp", Address: "127.0.0.1:1"}
	assert.Equal(t, SocketStartConnect, connect.CmdID())

	listen := &StartListenCmd{Network: "tcp", Address: "127.0.0.1:0"}
	assert.Equal(t, SocketStartListen, listen.CmdID())
}
