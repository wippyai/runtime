package pubsub

import (
	"github.com/ponyruntime/pony/api/payload"
	"sync"
)

// Package combines a Process ID with a batch of messages for tracking message origin
type Package struct {
	PID      PID
	From     PID // Added From field to track message origin
	Messages []*Message
}

var packagePool = sync.Pool{
	New: func() interface{} {
		return &Package{
			Messages: make([]*Message, 0, 1), // Pre-allocate space for 1 message
		}
	},
}

// AcquirePackage gets a Package from the pool or creates a new one
func AcquirePackage() *Package {
	return packagePool.Get().(*Package)
}

// ReleasePackage returns a Package to the pool after cleaning it
func ReleasePackage(p *Package) {
	if p == nil {
		return
	}

	// Clear the package fields
	p.PID = PID{}
	p.From = PID{}
	p.Messages = p.Messages[:0] // Preserve capacity but reset length

	packagePool.Put(p)
}

// NewPackage creates a new message batch with the specified topic and payload items
// This is now implemented using the pool
func NewPackage(pid PID, topic Topic, payloads ...payload.Payload) *Package {
	p := AcquirePackage()
	p.PID = pid
	p.Messages = append(p.Messages, &Message{
		Topic:    topic,
		Payloads: payloads,
	})
	return p
}

// AddMessage adds a new message to the package
func (p *Package) AddMessage(topic Topic, payloads ...payload.Payload) {
	p.Messages = append(p.Messages, &Message{
		Topic:    topic,
		Payloads: payloads,
	})
}
