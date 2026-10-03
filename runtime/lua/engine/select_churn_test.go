// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
)

func cleanupSelect(size int) (*Channel, *SelectOp) {
	c := NewChannel(0)
	s := &SelectOp{Cases: make([]*ChannelOp, size)}
	for i := range s.Cases {
		s.Cases[i] = &ChannelOp{Channel: NewChannel(0), SelectOp: s}
	}
	return c, s
}

func TestSelectCleanupReusesReleaseStorage(t *testing.T) {
	for _, size := range []int{2, 8, 64, 256} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			c, s := cleanupSelect(size)
			releases := make([]*Channel, 1, size+1)
			releases[0] = c
			base := &releases[0]
			allocs := testing.AllocsPerRun(100, func() {
				releases = c.flushSelect(s, releases[:1])
			})
			require.Zero(t, allocs, "select cleanup must use already-owned result storage")
			require.Same(t, base, &releases[0])
			require.Len(t, releases, size+1)
			for i, op := range s.Cases {
				require.Same(t, op.Channel, releases[i+1])
			}
		})
	}
}

func TestSelectCleanupPreservesReleaseOrderAndDuplicates(t *testing.T) {
	a, b, c := NewChannel(0), NewChannel(0), NewChannel(0)
	s := &SelectOp{Cases: []*ChannelOp{
		{Channel: b, Kind: ReceiveOp}, {Channel: nil}, {Channel: a, Kind: SendOp},
		{Channel: b, Kind: ReceiveOp}, {Channel: c, Kind: ReceiveOp},
	}}
	other := &SelectOp{}
	foreign := acquireChanOp()
	foreign.selectOp = other
	b.recvq.PushBack(foreign)
	for _, desc := range s.Cases {
		if desc.Channel == nil {
			continue
		}
		op := acquireChanOp()
		op.selectOp = s
		if desc.Kind == SendOp {
			desc.Channel.sendq.PushBack(op)
		} else {
			desc.Channel.recvq.PushBack(op)
		}
	}
	releases := c.flushSelect(s, []*Channel{c})
	require.Equal(t, []*Channel{c, b, a, b, c}, releases)
	for _, ch := range []*Channel{a, c} {
		require.Zero(t, ch.sendq.Len())
		require.Zero(t, ch.recvq.Len())
	}
	require.Zero(t, b.sendq.Len())
	require.Equal(t, 1, b.recvq.Len(), "cleanup must not remove another select's waiter")
	require.Same(t, foreign, b.recvq.Front().Value)
	b.recvq.Remove(b.recvq.Front())
	releaseChanOp(foreign)
	require.Equal(t, []*Channel{c}, c.flushSelect(nil, []*Channel{c}))
}

func TestSelectCasesCanBeSharedWithoutSharingResults(t *testing.T) {
	p := newChurnProcess(t, `
		local work, stop, done = channel.new(0), channel.new(0), channel.new(0)
		subscribe("work", work)
		local cases = {work:case_receive(), stop:case_receive()}
		local results = {}
		for i = 1, 2 do
			coroutine.spawn(function()
				results[i] = channel.select(cases)
				done:send(true)
			end)
		end
		done:receive()
		done:receive()
		assert(results[1] ~= results[2], "select results must be independent")
		assert(results[1].value ~= results[2].value, "waiters reused operation storage")
		assert(results[1].channel == work and results[2].channel == work)
		assert(results[1].ok and results[2].ok)
		local first, second = results[1].value, results[2].value
		assert(first + second == 3)
		work:close()
		local closed = channel.select(cases)
		assert(not closed.ok and closed.value == nil)
		assert(results[1].ok and results[1].value == first, "old result changed")
		assert(results[2].ok and results[2].value == second, "old result changed")
		return 2
	`)
	var out process.StepOutput
	require.NoError(t, p.Step([]process.Event{
		churnMessage("work", payload.NewPayload(lua.LInteger(1), payload.Lua)),
		churnMessage("work", payload.NewPayload(lua.LInteger(2), payload.Lua)),
	}, &out))
	require.Equal(t, process.StepDone, out.Status())
}

func TestSelectBindingDoesNotAllocateEachOperationSeparately(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	cases := selectReceiveCases(l, 64)
	cases.RawSetString("ignored", lua.LFalse) // isolate operation batching on the general iterator
	allocs := testing.AllocsPerRun(100, func() {
		l.SetTop(0)
		l.Push(cases)
		l.Push(lua.LTrue)
		require.Equal(t, 1, channelSelectFunc(l))
	})
	// The general iterator still boxes keys and default results are fresh.
	// Operation storage must not add another object per case on top of that.
	require.Less(t, allocs, float64(84))
}

func TestSelectBindingDoesNotBoxUnusedArrayKeys(t *testing.T) {
	for _, reservedHash := range []bool{false, true} {
		t.Run(fmt.Sprint(reservedHash), func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			cases := selectReceiveCases(l, 64)
			if reservedHash {
				cases.Strdict = make(map[string]lua.LValue)
				cases.Dict = make(map[lua.LValue]lua.LValue)
			}
			allocs := testing.AllocsPerRun(100, func() {
				l.SetTop(0)
				l.Push(cases)
				l.Push(lua.LTrue)
				require.Equal(t, 1, channelSelectFunc(l))
			})
			// Array case indices are never used by select. Keep fresh operation
			// and result storage without boxing an unused key for every index.
			require.Less(t, allocs, float64(20))
		})
	}
}

func TestSelectSparseAndNamedCases(t *testing.T) {
	for _, named := range []bool{false, true} {
		t.Run(fmt.Sprint(named), func(t *testing.T) {
			key := "1000"
			if named {
				key = `"work"`
			}
			p := newChurnProcess(t, `
				local work, stop = channel.new(0), channel.new(0)
				subscribe("work", work)
				local cases = {[`+key+`] = work:case_receive(), stop = stop:case_receive(), ignored = false}
				local result = channel.select(cases)
				assert(result.ok and result.channel == work and result.value == 1)
				return 1
			`)
			var out process.StepOutput
			require.NoError(t, p.Step([]process.Event{
				churnMessage("work", payload.NewPayload(lua.LInteger(1), payload.Lua)),
			}, &out))
			require.Equal(t, process.StepDone, out.Status())
		})
	}
}

func TestSelectLargeWaiterCleanup(t *testing.T) {
	for _, size := range []int{1, 2, 8, 9, 64, 256} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			p := newChurnProcess(t, fmt.Sprintf(`
				channels = {}
				local cases, gate = {}, channel.new(0)
				for i = 1, %d do
					channels[i] = channel.new(0)
					cases[i] = channels[i]:case_receive()
				end
				subscribe("work", channels[1])
				subscribe("gate", gate)
				local result = channel.select(cases)
				assert(result.ok and result.value == 1 and result.channel == channels[1])
				gate:receive()
			`, size))
			var out process.StepOutput
			require.NoError(t, p.Step([]process.Event{
				churnMessage("work", payload.NewPayload(lua.LInteger(1), payload.Lua)),
			}, &out))
			require.Equal(t, process.StepIdle, out.Status())
			channels := p.State().GetGlobal("channels").(*lua.LTable)
			for i := 1; i <= size; i++ {
				ch := channels.RawGetInt(i).(*lua.LUserData).Value.(*Channel)
				require.Zero(t, ch.sendq.Len())
				require.Zero(t, ch.recvq.Len())
				require.Zero(t, p.channels[ch], "retired select must release every channel reference")
			}
		})
	}
}

func TestSelectIgnoresNonCaseArrayValues(t *testing.T) {
	for _, namedExtra := range []bool{false, true} {
		t.Run(fmt.Sprint(namedExtra), func(t *testing.T) {
			p := newChurnProcess(t, fmt.Sprintf(`
				local work, stop = channel.new(0), channel.new(0)
				subscribe("work", work)
				local cases = {}
				for i = 1, 64 do cases[i] = false end
				cases[32], cases[64] = nil, nil
				cases[63] = work:case_receive()
				if %t then cases.extra = stop:case_receive() end
				local result = channel.select(cases)
				assert(result.ok and result.channel == work and result.value == 1)
				return 1
			`, namedExtra))
			var out process.StepOutput
			require.NoError(t, p.Step([]process.Event{
				churnMessage("work", payload.NewPayload(lua.LInteger(1), payload.Lua)),
			}, &out))
			require.Equal(t, process.StepDone, out.Status())
		})
	}
}

func selectReceiveCases(l *lua.LState, size int) *lua.LTable {
	cases := l.CreateTable(size, 0)
	for i := 1; i <= size; i++ {
		ud := l.NewUserData()
		ud.Value = &SelectCase{Channel: NewChannel(0), Kind: ReceiveOp}
		cases.RawSetInt(i, ud)
	}
	return cases
}

func BenchmarkSelectCleanup(b *testing.B) {
	for _, size := range []int{2, 8, 64, 256} {
		for _, warm := range []bool{false, true} {
			b.Run(fmt.Sprintf("cases=%d/warm=%t", size, warm), func(b *testing.B) {
				c, s := cleanupSelect(size)
				var releases []*Channel
				if warm {
					releases = make([]*Channel, 1, size+1)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					if warm {
						releases = releases[:1]
					} else {
						releases = make([]*Channel, 1, 4)
					}
					releases[0] = c
					releases = c.flushSelect(s, releases)
				}
				if len(releases) != size+1 {
					b.Fatal("incorrect cleanup result")
				}
			})
		}
	}
}

func BenchmarkSelectBindingDefault(b *testing.B) {
	for _, size := range []int{2, 8, 64, 256} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			l := lua.NewState()
			defer l.Close()
			cases := selectReceiveCases(l, size)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				l.SetTop(0)
				l.Push(cases)
				l.Push(lua.LTrue)
				if channelSelectFunc(l) != 1 {
					b.Fatal("expected default result")
				}
			}
		})
	}
}
