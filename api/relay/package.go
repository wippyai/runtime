// Package relay provides message relay and routing system for inter-component communication.
package relay

import (
	"sync"

	"github.com/wippyai/runtime/api/payload"
)

// Package combines a Process Source with a batch of messages for tracking message origin.
// It serves as the primary container for message delivery in the pub/sub system.
type Package struct {
	// Source identifies the process that generated the messages
	Source PID
	// Target identifies the process that should receive the messages
	Target PID
	// Messages contains the collection of messages in this package
	Messages []*Message
}

// Object pool for Package instances to reduce memory allocation overhead
//

var packagePool = sync.Pool{
	New: func() interface{} {
		return &Package{
			Messages: make([]*Message, 0, 1), // Pre-allocate space for 1 message
		}
	},
}

// AcquirePackage gets a Package from the pool or creates a new one.
// This helps reduce memory allocations and garbage collection overhead.
func AcquirePackage() *Package {
	return packagePool.Get().(*Package)
}

// ReleasePackage returns a Package to the pool after cleaning it.
// This should be called when the package is no longer needed to enable reuse.
func ReleasePackage(p *Package) {
	if p == nil {
		return
	}

	// Clear the package fields
	p.Source = PID{} // Reset source to empty PID
	p.Target = PID{}
	p.Messages = p.Messages[:0] // Preserve capacity but reset length

	packagePool.Put(p)
}

// NewPackage creates a new message package with the specified process ID, topic, and payload items.
// This is implemented using the object pool to improve performance.
func NewPackage(source, pid PID, topic Topic, payloads ...payload.Payload) *Package {
	p := AcquirePackage()
	p.Source = source
	p.Target = pid
	p.Messages = append(p.Messages, &Message{
		Topic:    topic,
		Payloads: payloads,
	})
	return p
}

func NewMessagePackage(source, pid PID, msg ...*Message) *Package {
	p := AcquirePackage()
	p.Source = source
	p.Target = pid
	p.Messages = msg
	return p
}

// AddMessage adds a new message to the package with the specified topic and payload items.
// Multiple messages with different topics can be added to a single package.
func (p *Package) AddMessage(topic Topic, payloads ...payload.Payload) {
	p.Messages = append(p.Messages, &Message{
		Topic:    topic,
		Payloads: payloads,
	})
}
