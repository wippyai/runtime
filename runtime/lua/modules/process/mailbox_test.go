// SPDX-License-Identifier: MPL-2.0

package process

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

func TestListenPreservesMailboxBuffer(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	l.Push(lua.LString("work"))
	require.Equal(t, -1, listen(l))
	req, ok := l.Get(-1).(*engine.SubscribeRequest)
	require.True(t, ok)
	require.Equal(t, 1, req.BufSize, "nonblocking select must still observe buffered messages")
	require.True(t, req.RetainOnUpgrade)
}
