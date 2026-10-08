// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"cmp"
	"container/list"
	"fmt"
	"slices"
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
)

// mailboxEnvelope is the replayable part of a delivered message. Queue budgets
// and retention leases are released at delivery and must not be copied here.
type mailboxEnvelope struct {
	Source   pid.PID
	Topic    string
	Payloads []payload.Payload
	sequence uint64
}

func (m mailboxEnvelope) queuedMessage() queuedMessage {
	return queuedMessage{Source: m.Source, Topic: m.Topic, Payloads: m.Payloads, sequence: m.sequence}
}

// bufferedDelivery stays private to a channel buffer or pending receive. The
// buffer, channel result and task move ownership in that order. The wrapper is
// returned to the pool only when consumed or discarded, never exposed to Lua.
type bufferedDelivery struct {
	lua.LValue
	message mailboxEnvelope
}

var bufferedDeliveryPool = sync.Pool{New: func() any { return &bufferedDelivery{} }}

func acquireBufferedDelivery(value lua.LValue, message mailboxEnvelope) *bufferedDelivery {
	delivery := bufferedDeliveryPool.Get().(*bufferedDelivery)
	delivery.LValue, delivery.message = value, message
	return delivery
}

func releaseBufferedDelivery(delivery *bufferedDelivery) {
	delivery.LValue = nil
	delivery.message = mailboxEnvelope{}
	bufferedDeliveryPool.Put(delivery)
}

func (t *Task) releaseDelivery() {
	if t.delivery != nil {
		releaseBufferedDelivery(t.delivery)
		t.delivery = nil
	}
}

func (t *Task) takeDelivery(update *TaskUpdate) {
	if update.delivery != nil {
		t.releaseDelivery()
		t.delivery, update.delivery = update.delivery, nil
	}
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
			task.takeDelivery(result.Updates[0])
		}
	}
}

func (p *Process) captureUpgradeMessages() {
	clear(p.upgradeMessages)
	p.upgradeMessages = p.upgradeMessages[:0]
	for _, task := range p.threads {
		if task.delivery != nil {
			p.upgradeMessages = append(p.upgradeMessages, task.delivery.message.queuedMessage())
		}
	}
	if p.subs == nil {
		return
	}
	seen := make(map[*list.List]struct{})
	for _, sub := range p.subs.snapshotSubscriptions() {
		if !sub.retainOnUpgrade {
			continue
		}
		// Go-side subscriptions can share a channel, including native value
		// copies whose queue pointers alias. Capture each buffer only once.
		if _, exists := seen[sub.channel.buffer]; exists {
			continue
		}
		seen[sub.channel.buffer] = struct{}{}
		for e := sub.channel.buffer.Front(); e != nil; e = e.Next() {
			if delivery, ok := e.Value.(*bufferedDelivery); ok {
				p.upgradeMessages = append(p.upgradeMessages, delivery.message.queuedMessage())
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
