// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"strings"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/topology"
)

// For large interleaved data bursts, keep FIFO offsets into the existing
// mailbox for one Step. A receive clears its slot immediately. Compact once
// before returning to the scheduler or to general mailbox delivery.
const minSelectBatch = 64

// Positions are one-based so zero marks the end of a channel's FIFO.
type selectDataBatch struct {
	first map[*Channel]int
	next  []int
}

func (p *Process) indexSelectData(subs *subscribeContext) *selectDataBatch {
	var batch *selectDataBatch
	for i := len(p.messageQueue) - 1; i >= 0; i-- {
		qm := p.messageQueue[i]
		if qm.sequence == 0 || messageIsBounded(qm) {
			return nil
		}
		if _, framed := subscriptionFrameFromPayloads(qm.Payloads); framed {
			return nil
		}
		for _, pl := range qm.Payloads {
			if payload.IsTerminal(pl) {
				return nil
			}
		}
		if qm.Topic == topology.TopicEvents {
			if _, outdated := outdatedEventFromPayloads(qm.Payloads); outdated || isLinkDownEvent(qm.Payloads) {
				return nil
			}
		}
		sub, ok := subs.match(qm.Topic)
		if !ok {
			if strings.HasPrefix(qm.Topic, "@") {
				continue
			}
			sub, ok = subs.get(topology.TopicInbox)
			if !ok {
				continue
			}
		}
		// An immediate receive can stage one value. Larger buffers can
		// admit several values and must retain the normal flush behavior.
		if sub.channel.capacity > 1 || sub.channel.IsClosed() {
			return nil
		}
		if _, hasHandler := p.GetTopicHandler(sub.topic); hasHandler {
			return nil
		}
		if batch == nil {
			batch = &selectDataBatch{first: make(map[*Channel]int), next: make([]int, len(p.messageQueue))}
		}
		batch.next[i] = batch.first[sub.channel]
		batch.first[sub.channel] = i + 1
	}
	return batch
}

func (p *Process) receiveSelectData(subs *subscribeContext, ch *Channel) {
	first := p.selectData.first[ch]
	if first == 0 {
		return
	}
	i := first - 1
	qm := p.messageQueue[i]
	// Transcoding can run a producer cleanup. Defer queue compaction
	// until this slot has been accounted for, as in a general flush.
	p.flushingMessages = true
	keep := p.deliverMessage(subs, ch, qm)
	p.flushingMessages = false
	if len(p.messageQueueDiscarded) != 0 {
		p.finishDiscardedTopics()
	}
	if keep {
		return
	}
	p.releaseQueuedMessage(qm)
	p.messageQueue[i] = queuedMessage{}
	next := p.selectData.next[i]
	if next == 0 {
		delete(p.stalledChans, ch)
		delete(p.selectData.first, ch)
	} else {
		p.selectData.first[ch] = next
		p.stalledChans[ch] = p.messageQueue[next-1].sequence
	}
}

func (p *Process) finishSelectDataBatch() {
	p.canIndexSelect = false
	if p.selectData == nil {
		return
	}
	p.selectData = nil
	queued := p.messageQueue
	remaining := queued[:0]
	for _, qm := range queued {
		if qm.sequence != 0 {
			remaining = append(remaining, qm)
		}
	}
	clear(queued[len(remaining):])
	p.messageQueue = remaining
}
