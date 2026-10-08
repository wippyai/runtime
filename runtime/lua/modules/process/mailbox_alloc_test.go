// SPDX-License-Identifier: MPL-2.0

//go:build !race

package process_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	procapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
)

// Pool allocation counts are not stable with the race detector. Delivery and
// handler-policy ownership are covered separately by ordinary race-safe tests.
func TestBlockedInboxRetriesDoNotAllocateMessageWrappers(t *testing.T) {
	p := newBlockedInboxProcess(t)
	var out procapi.StepOutput
	allocs := testing.AllocsPerRun(100, func() {
		out.Reset()
		if err := p.Step(nil, &out); err != nil {
			t.Fatal(err)
		}
	})
	require.Zero(t, allocs, "an unread inbox must not construct discarded Message wrappers on every retry")
}

func BenchmarkBlockedInboxRetries(b *testing.B) {
	p := newBlockedInboxProcess(b)
	var out procapi.StepOutput
	b.ReportAllocs()
	for b.Loop() {
		out.Reset()
		if err := p.Step(nil, &out); err != nil {
			b.Fatal(err)
		}
	}
}

func newBlockedInboxProcess(t testing.TB) *engine.Process {
	t.Helper()
	ctx, frame := ctxapi.OpenFrameContext(security.SetStrictMode(ctxapi.NewRootContext(), false))
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(frame) })
	p, err := engine.NewProcess(engine.WithScript(`
		return { main = function()
			local inbox, gate = process.inbox(), channel.new(0)
			gate:receive()
		end }
	`, "blocked_inbox.lua"), engine.WithModuleBinder(func(l *lua.LState) error {
		engine.LoadModuleDef(l, engine.ChannelModule)
		mod, _ := processmod.Module.Build()
		l.SetGlobal("process", mod)
		return nil
	}))
	require.NoError(t, err)
	t.Cleanup(func() { p.Close() })
	require.NoError(t, p.Init(ctx, "main", nil))
	var out procapi.StepOutput
	require.NoError(t, p.Step(nil, &out))
	pkg := relay.AcquirePackage()
	msg := relay.AcquireMessage()
	msg.Topic = "work"
	msg.Payloads = payload.Payloads{payload.NewPayload(lua.LInteger(1), payload.Lua)}
	pkg.Messages = append(pkg.Messages, msg)
	require.NoError(t, p.Step([]procapi.Event{{Type: procapi.EventMessage, Data: pkg}}, &out))
	require.Equal(t, procapi.StepIdle, out.Status())
	return p
}
