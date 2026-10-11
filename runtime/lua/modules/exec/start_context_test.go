// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

type contextStartupProcess struct {
	received context.Context
	mockProcess
}

func (p *contextStartupProcess) StartContext(ctx context.Context) error {
	p.received = ctx
	return ctx.Err()
}

func TestProcStartUsesLifecycleContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)
	handle := &contextStartupProcess{}
	p := NewProcess(ctx, handle)
	ud := l.NewUserData()
	ud.Value = p
	l.Push(ud)
	require.Equal(t, 2, procStart(l))
	require.Same(t, ctx, handle.received)
	require.Equal(t, lua.LNil, l.Get(-2))
	require.False(t, p.started)
	require.True(t, p.closed)
}
