package relay

import (
	"sync"

	"github.com/wippyai/runtime/api/payload"
)

var packagePool = sync.Pool{
	New: func() interface{} {
		return &Package{
			Messages: make([]*Message, 0, 1),
		}
	},
}

// AcquirePackage gets a Package from the pool.
func AcquirePackage() *Package {
	return packagePool.Get().(*Package)
}

// ReleasePackage returns a Package to the pool.
func ReleasePackage(p *Package) {
	if p == nil {
		return
	}
	p.Source = PID{}
	p.Target = PID{}
	p.Messages = p.Messages[:0]
	packagePool.Put(p)
}

// NewPackage creates a new package with source, target, topic and payloads.
func NewPackage(source, target PID, topic Topic, payloads ...payload.Payload) *Package {
	p := AcquirePackage()
	p.Source = source
	p.Target = target
	p.Messages = append(p.Messages, &Message{
		Topic:    topic,
		Payloads: payloads,
	})
	return p
}

// NewMessagePackage creates a new package with pre-built messages.
func NewMessagePackage(source, target PID, msgs ...*Message) *Package {
	p := AcquirePackage()
	p.Source = source
	p.Target = target
	p.Messages = msgs
	return p
}
