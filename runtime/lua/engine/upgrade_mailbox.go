// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"cmp"
	"fmt"
	"slices"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
)

// bufferedDelivery stays private to the channel buffer. Receive unwraps it
// before returning a value, so application channels retain their usual surface.
type bufferedDelivery struct {
	lua.LValue
	message queuedMessage
}

// A buffered receive can itself yield (for example to wake a blocked sender).
// Remember its envelope before returning to vmStep: another ready coroutine
// may request upgrade before processChannelYields applies the channel result.
func rememberUpgradeDelivery(l *lua.LState, result *ChannelResult) {
	if len(result.Updates) == 0 || result.Updates[0].delivery == nil {
		return
	}
	if p := GetProcess(l); p != nil {
		if task, err := p.GetTask(l); err == nil {
			task.delivery = result.Updates[0].delivery
		}
	}
}

func (p *Process) captureUpgradeMessages() {
	clear(p.upgradeMessages)
	p.upgradeMessages = p.upgradeMessages[:0]
	for _, task := range p.threads {
		if task.delivery != nil {
			p.upgradeMessages = append(p.upgradeMessages, task.delivery.message)
		}
	}
	if p.subs == nil {
		return
	}
	for _, sub := range p.subs.snapshotSubscriptions() {
		if !sub.retainOnUpgrade {
			continue
		}
		for e := sub.channel.buffer.Front(); e != nil; e = e.Next() {
			if delivery, ok := e.Value.(*bufferedDelivery); ok {
				p.upgradeMessages = append(p.upgradeMessages, delivery.message)
			}
		}
	}
}

// TransferUpgradeState moves only unread message envelopes. Subscriptions,
// handlers, tasks and producer frames belong to the old code incarnation.
func (p *Process) TransferUpgradeState(replacement process.Process) error {
	next, luaTarget := replacement.(*Process)
	if next == p {
		return fmt.Errorf("replacement must be a different process incarnation")
	}
	if luaTarget {
		// The scheduler queue survives the swap. Even an empty mailbox can have
		// old producer frames still in flight. Do not let a fresh/recycled VM's
		// epoch and subscription ids accidentally match those old frames.
		next.epoch.Store(max(next.epoch.Load(), p.epoch.Load()+1))
	}
	if len(p.messageQueue) == 0 && len(p.upgradeMessages) == 0 {
		return nil
	}
	messages := make([]queuedMessage, 0, len(p.upgradeMessages)+len(p.messageQueue))
	messages = append(messages, p.upgradeMessages...)
	for _, qm := range p.messageQueue {
		if _, framed := subscriptionFrameFromPayloads(qm.Payloads); !framed {
			messages = append(messages, qm)
		}
	}
	if len(messages) == 0 {
		return nil
	}
	if !luaTarget {
		return fmt.Errorf("replacement %T cannot accept a Lua process mailbox", replacement)
	}
	if len(next.messageQueue) != 0 {
		return fmt.Errorf("replacement mailbox is not empty")
	}
	// Subscription maps have no iteration order; buffered deliveries and the
	// retained tail must be restored in their original admission order.
	slices.SortFunc(messages, func(a, b queuedMessage) int { return cmp.Compare(a.sequence, b.sequence) })
	for i := range messages {
		// Use the canonical export boundary for Lua payloads, preserving their
		// shape without carrying receiver-owned tables into the replacement.
		cloned := false
		for j, pl := range messages[i].Payloads {
			if pl != nil && pl.Format() == payload.Lua {
				if lv, ok := pl.Data().(lua.LValue); ok {
					if !cloned {
						messages[i].Payloads = slices.Clone(messages[i].Payloads)
						cloned = true
					}
					messages[i].Payloads[j] = luaconv.ExportPayload(lv)
				}
			}
		}
	}
	for _, qm := range p.messageQueue {
		if _, framed := subscriptionFrameFromPayloads(qm.Payloads); framed {
			p.releaseQueuedMessage(qm)
		}
	}

	// Move reservations rather than re-admitting already accepted messages:
	// applying limits again could discard a backlog that fit the old buffer.
	next.messageQueue = messages
	next.messageSeq = p.messageSeq
	next.messageQueueItems, p.messageQueueItems = p.messageQueueItems, nil
	next.messageQueueBytes, p.messageQueueBytes = p.messageQueueBytes, nil
	next.messageQueueItemLimits, p.messageQueueItemLimits = p.messageQueueItemLimits, nil
	next.messageQueueLimits, p.messageQueueLimits = p.messageQueueLimits, nil
	next.messageQueueOverflowed, p.messageQueueOverflowed = p.messageQueueOverflowed, nil
	next.messageQueueDiscarded, p.messageQueueDiscarded = p.messageQueueDiscarded, nil
	clear(p.messageQueue)
	p.messageQueue = p.messageQueue[:0]
	clear(p.upgradeMessages)
	p.upgradeMessages = nil
	return nil
}
