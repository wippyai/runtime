// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"encoding/binary"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// Refusals retain only a correlation and destination, never the rejected blob.
// A single joined worker keeps response backpressure off the native reader.
// If this bounded queue is also full, delivery fails without taking ownership;
// a remote caller must then treat a missing reply as uncertain.
type forwardRefusal struct {
	node pid.NodeID
	corr uint64
	read bool
}

// Caller holds forwardAdmission, serializing producers and shutdown. Reserve
// the complete batch before accepting ownership; never partially refuse it.
func (e *RaftEngine) refuseForwardLocked(pkg *relay.Package) error {
	count := 0
	for _, msg := range pkg.Messages {
		if msg.Topic != topicKVForwardReq && msg.Topic != topicKVReadReq {
			continue
		}
		if len(msg.Payloads) == 0 {
			continue
		}
		body, ok := msg.Payloads[0].Data().([]byte)
		if ok && len(body) >= 9 {
			count++
		}
	}
	if count > cap(e.forwardRefusals)-len(e.forwardRefusals) {
		return kvapi.ErrOverloaded
	}
	for _, msg := range pkg.Messages {
		if msg.Topic != topicKVForwardReq && msg.Topic != topicKVReadReq {
			continue
		}
		if len(msg.Payloads) == 0 {
			continue
		}
		body, ok := msg.Payloads[0].Data().([]byte)
		if ok && len(body) >= 9 {
			e.forwardRefusals <- forwardRefusal{node: pkg.Source.Node, corr: binary.BigEndian.Uint64(body[:8]), read: msg.Topic == topicKVReadReq}
		}
	}
	relay.ReleasePackage(pkg)
	return nil
}

func (e *RaftEngine) refusalLoop() {
	defer e.wg.Done()
	for {
		select {
		case <-e.ctx.Done():
			return
		case refusal := <-e.forwardRefusals:
			if e.ctx.Err() != nil {
				return
			}
			if refusal.read {
				e.replyRead(refusal.node, refusal.corr, readResult{err: kvapi.ErrOverloaded})
			} else {
				e.replyForward(refusal.node, refusal.corr, applyResult{Err: kvapi.ErrOverloaded})
			}
		}
	}
}
