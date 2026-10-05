// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"strings"

	"github.com/wippyai/runtime/api/payload"
)

// beginMessageBatch recognizes an ordinary, single-topic data burst once per
// Step. Such a burst can consume its prefix without rescanning or compacting
// the retained tail after every rendezvous receive. Control messages, bounded
// producers and handlers stay on the general delivery path.
//
// Step admits all new events before this point. Only the step goroutine owns
// the mailbox, and no new messages are enqueued during the batch.
func (p *Process) beginMessageBatch() []queuedMessage {
	queued := p.messageQueue
	if len(queued) < 2 || p.subs == nil || len(p.messageQueueDiscarded) > 0 {
		return nil
	}
	topic := queued[0].Topic
	if strings.HasPrefix(topic, "@") {
		return nil
	}
	if _, exists := p.subs.match(topic); !exists {
		return nil // preserve inbox fallback and late subscription semantics
	}
	if _, exists := p.GetTopicHandler(topic); exists {
		return nil
	}
	for _, qm := range queued {
		if qm.Topic != topic || messageIsBounded(qm) {
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
	}
	p.messageBatchTopic = topic
	p.messageBatchActive = true
	return queued
}

// finishMessageBatch compacts at most once, restoring all of the reusable
// capacity even when the fast path consumed the entire queue. Cleared prefix
// slots never retain delivered payloads while the actor is blocked.
func (p *Process) finishMessageBatch(base []queuedMessage) {
	n := len(p.messageQueue)
	copy(base, p.messageQueue)
	clear(base[n:])
	p.messageQueue = base[:n]
	p.messageBatchActive = false
	p.messageBatchTopic = ""
}

func (p *Process) flushMessageBatch(subs *subscribeContext) bool {
	if !p.messageBatchActive {
		return false
	}
	// Lua may unsubscribe, replace the channel or install a handler between
	// flushes. Never cache the subscription or bypass those changes.
	sub, exists := subs.match(p.messageBatchTopic)
	_, hasHandler := p.GetTopicHandler(p.messageBatchTopic)
	if !exists || hasHandler || sub.channel.IsClosed() {
		p.messageBatchActive = false
		return false
	}
	clear(p.stalledChans)
	for len(p.messageQueue) > 0 && sub.channel.CanSend() {
		qm := p.messageQueue[0]
		if p.deliverMessage(subs, qm) {
			break
		}
		p.releaseQueuedMessage(qm)
		p.messageQueue[0] = queuedMessage{}
		p.messageQueue = p.messageQueue[1:]
	}
	return true
}
