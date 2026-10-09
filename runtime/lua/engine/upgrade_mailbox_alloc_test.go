// SPDX-License-Identifier: MPL-2.0

//go:build !race

package engine

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
)

// The race detector deliberately drops sync.Pool objects, making exact
// allocation comparisons meaningless. Ownership tests still run with -race.
func TestUpgradeDeliveryDoesNotAddPerMessageAllocations(t *testing.T) {
	for _, capacity := range []int{0, 1} {
		t.Run(map[int]string{0: "rendezvous", 1: "buffered"}[capacity], func(t *testing.T) {
			p := newCDCRegressionProcess(t)
			defer p.Close()
			sub, err := p.subs.add("data", capacity)
			require.NoError(t, err)
			task := p.createTask(p.state.NewFunction(func(*lua.LState) int { return 0 }))
			pls := payload.Payloads{payload.NewPayload(lua.LString("value"), payload.Lua)}
			measure := func(retain bool) float64 {
				sub.retainOnUpgrade = retain
				return testing.AllocsPerRun(100, func() {
					if capacity == 0 {
						ReleaseResult(sub.channel.Receive(task.Thread(), nil))
					}
					p.enqueueMessage(queuedMessage{Topic: "data", Payloads: pls})
					p.flushMessageQueue(p.subs)
					if capacity != 0 {
						p.applyExternalChannelResult(sub.channel.Receive(task.Thread(), nil))
					}
					p.queue.Pop()
					task.releaseDelivery()
					clear(task.resumeBuf)
					task.Resumed = nil
				})
			}
			ordinary, retained := measure(false), measure(true)
			require.LessOrEqual(t, retained, ordinary, "upgrade continuity must not add per-message envelope allocations")
		})
	}
}
