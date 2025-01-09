package channel

import "sync"

var (
	signalPool = sync.Pool{New: func() interface{} { return &signalEntry{} }}
)

// inbox optimized for ordered tracking of inbox channel receivers
type inbox struct {
	// Slice of channel entries, maintained in registration order
	channels []*signalEntry
}

// signalEntry tracks receivers for a single inbox channel
type signalEntry struct {
	name      string
	receivers *pendingQueue
}

func (s *signalEntry) reset() {
	s.name = ""
	s.receivers = nil
}

func newInbox() *inbox {
	return &inbox{
		channels: make([]*signalEntry, 0),
	}
}

func (ec *inbox) addReceiver(name string, op *pendingOp) {
	for i := range ec.channels {
		if ec.channels[i].name == name {
			if ec.channels[i].receivers == nil {
				ec.channels[i].receivers = queuePool.Get().(*pendingQueue)
				ec.channels[i].receivers.reset()
			}

			// Add to tail
			if ec.channels[i].receivers.tail == nil {
				ec.channels[i].receivers.head = op
				ec.channels[i].receivers.tail = op
			} else {
				ec.channels[i].receivers.tail.next = op
				ec.channels[i].receivers.tail = op
			}
			return
		}
	}

	// New channel
	queue := queuePool.Get().(*pendingQueue)
	queue.head = op
	queue.tail = op

	entry := signalPool.Get().(*signalEntry)
	entry.name = name
	entry.receivers = queue

	ec.channels = append(ec.channels, entry)
}

func (ec *inbox) removeReceiver(name string, op *pendingOp) {
	for i := range ec.channels {
		if ec.channels[i].name == name {
			// Remove receiver
			queue := ec.channels[i].receivers

			// Find and remove op from queue
			if queue.head == op {
				queue.head = op.next
				if queue.head == nil {
					queue.tail = nil
				}
			} else {
				// Search through queue
				for curr := queue.head; curr != nil && curr.next != nil; curr = curr.next {
					if curr.next == op {
						curr.next = op.next
						if curr.next == nil {
							queue.tail = curr
						}
						break
					}
				}
			}

			// If queue empty, remove channel entry and return queue to pool
			if queue.head == nil {
				lastIdx := len(ec.channels) - 1
				entry := ec.channels[i]
				ec.channels[i] = ec.channels[lastIdx]
				ec.channels = ec.channels[:lastIdx]

				// Return both queue and signal entry to pools
				queue.reset()
				queuePool.Put(queue)
				entry.reset()
				signalPool.Put(entry)
			}
			return
		}
	}
}

func (ec *inbox) getNames() []string {
	if len(ec.channels) == 0 {
		return nil
	}
	names := make([]string, len(ec.channels))
	for i := range ec.channels {
		names[i] = ec.channels[i].name
	}
	return names
}

func (ec *inbox) popReceiver(name string) *pendingOp {
	for i := range ec.channels {
		if ec.channels[i].name == name {
			queue := ec.channels[i].receivers
			if queue != nil {
				// Get first from queue
				op := queue.head
				if op != nil {
					// Update queue
					queue.head = op.next
					if queue.head == nil {
						queue.tail = nil

						// Queue is empty, clean up
						lastIdx := len(ec.channels) - 1
						ec.channels[i] = ec.channels[lastIdx]
						ec.channels = ec.channels[:lastIdx]

						// Return queue to pool
						queue.reset()
						queuePool.Put(queue)
					}
					op.next = nil
				}
				return op
			}
		}
	}
	return nil
}
