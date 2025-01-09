package channel

import "sync"

var (
	signalPool = sync.Pool{New: func() interface{} { return &signalEntry{} }}
)

type inbox struct {
	entries []*signalEntry
}

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
		entries: make([]*signalEntry, 0),
	}
}

// removeEntry removes the entry at the given index and returns it
// Uses swap-with-last and pop pattern for O(1) removal
func (ec *inbox) removeEntry(idx int) *signalEntry {
	lastIdx := len(ec.entries) - 1
	entry := ec.entries[idx]
	ec.entries[idx] = ec.entries[lastIdx]
	ec.entries = ec.entries[:lastIdx]
	return entry
}

func (ec *inbox) findChannel(name string) (int, *signalEntry) {
	for i := range ec.entries {
		if ec.entries[i].name == name {
			return i, ec.entries[i]
		}
	}
	return -1, nil
}

func (ec *inbox) addReceiver(name string, op *pendingOp) {
	idx, entry := ec.findChannel(name)

	if idx >= 0 {
		if entry.receivers == nil {
			entry.receivers = queuePool.Get().(*pendingQueue)
			entry.receivers.reset()
		}
		entry.receivers.enqueue(op)
		return
	}

	// New channel entry
	queue := queuePool.Get().(*pendingQueue)
	queue.reset()
	queue.enqueue(op)

	entry = signalPool.Get().(*signalEntry)
	entry.name = name
	entry.receivers = queue

	ec.entries = append(ec.entries, entry)
}

func (ec *inbox) removeReceiver(name string, op *pendingOp) {
	idx, entry := ec.findChannel(name)
	if idx < 0 {
		return
	}

	queue := entry.receivers
	if queue == nil {
		return
	}

	if queue.remove(op) && queue.size == 0 {
		entry = ec.removeEntry(idx)
		queue.reset()
		queuePool.Put(queue)
		entry.reset()
		signalPool.Put(entry)
	}
}

func (ec *inbox) popReceiver(name string) *pendingOp {
	idx, entry := ec.findChannel(name)
	if idx < 0 {
		return nil
	}

	queue := entry.receivers
	if queue == nil {
		return nil
	}

	op := queue.dequeue()
	if op == nil {
		return nil
	}

	if queue.size == 0 {
		entry = ec.removeEntry(idx)
		queue.reset()
		queuePool.Put(queue)
		entry.reset()
		signalPool.Put(entry)
	}
	return op
}

func (ec *inbox) getNames() []string {
	if len(ec.entries) == 0 {
		return nil
	}
	names := make([]string, len(ec.entries))
	for i := range ec.entries {
		names[i] = ec.entries[i].name
	}
	return names
}

// clear empties the inbox and properly cleans up all resources
func (ec *inbox) clear() {
	for _, entry := range ec.entries {
		if queue := entry.receivers; queue != nil {
			queue.clear() // This will return ops to pendingPool
			queuePool.Put(queue)
		}
		entry.reset()
		signalPool.Put(entry)
	}
	ec.entries = ec.entries[:0]
}
