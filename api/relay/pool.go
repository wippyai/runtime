// SPDX-License-Identifier: MPL-2.0

package relay

import (
	"sync"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
)

var packagePool = sync.Pool{
	New: func() any {
		return &Package{
			Messages: make([]*Message, 0, 1),
		}
	},
}

var messagePool = sync.Pool{
	New: func() any {
		return &Message{}
	},
}

// AcquirePackage gets a Package from the pool.
func AcquirePackage() *Package {
	return packagePool.Get().(*Package)
}

// AcquireMessage gets a Message from the pool.
func AcquireMessage() *Message {
	return messagePool.Get().(*Message)
}

// ReleaseMessage returns a Message to the pool.
func ReleaseMessage(m *Message) {
	if m == nil {
		return
	}
	m.Topic = ""
	m.Payloads = nil
	messagePool.Put(m)
}

// ReleasePackage returns a Package to the pool.
// Also releases all messages back to their pool.
func ReleasePackage(p *Package) {
	if p == nil {
		return
	}
	for _, msg := range p.Messages {
		ReleaseMessage(msg)
	}
	p.Source = pid.PID{}
	p.Target = pid.PID{}
	p.Messages = p.Messages[:0]
	packagePool.Put(p)
}

// NewPackage creates a new package with source, target, topic and payloads.
func NewPackage(source, target pid.PID, topic Topic, payloads ...payload.Payload) *Package {
	p := AcquirePackage()
	p.Source = source
	p.Target = target
	msg := AcquireMessage()
	msg.Topic = topic
	msg.Payloads = payloads
	p.Messages = append(p.Messages, msg)
	return p
}

// NewServicePackage creates a package addressed between service hosts.
// Unlike process PIDs, service addresses use empty UniqID since relay
// routes host-level receivers by Target.Host alone.
func NewServicePackage(sourceNode pid.NodeID, sourceHost pid.HostID, targetNode pid.NodeID, targetHost pid.HostID, topic Topic, payloads ...payload.Payload) *Package {
	p := AcquirePackage()
	src := pid.PID{Node: sourceNode, Host: sourceHost}
	tgt := pid.PID{Node: targetNode, Host: targetHost}
	p.Source = src.Precomputed()
	p.Target = tgt.Precomputed()
	msg := AcquireMessage()
	msg.Topic = topic
	msg.Payloads = payloads
	p.Messages = append(p.Messages, msg)
	return p
}

// NewMessagePackage creates a new package with pre-built messages.
func NewMessagePackage(source, target pid.PID, msgs ...*Message) *Package {
	p := AcquirePackage()
	p.Source = source
	p.Target = target
	p.Messages = msgs
	return p
}
