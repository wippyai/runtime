package helper

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/topology"
	"log"
	"time"
)

type MessageHandler func(msg *pubsub.Message) error

// Monitor provides a simple way to attach to a node and handle messages
type Monitor struct {
	node    pubsub.Host
	pid     pubsub.PID
	handler MessageHandler
	detach  context.CancelFunc
	eventCh chan *pubsub.Batch
	stopCh  chan struct{}
}

// NewMonitor creates a new monitor for the given node and PID
func NewMonitor(node pubsub.Host, pid pubsub.PID, handler MessageHandler) *Monitor {
	return &Monitor{
		node:    node,
		pid:     pid,
		handler: handler,
		eventCh: make(chan *pubsub.Batch, 10),
		stopCh:  make(chan struct{}),
	}
}

func (m *Monitor) PID() pubsub.PID {
	return m.pid
}

// Start begins monitoring messages
func (m *Monitor) Start() error {
	var err error
	err, m.detach = m.node.Attach(m.pid, m.eventCh)
	if err != nil {
		return fmt.Errorf("failed to attach to node: %w", err)
	}

	go m.run()
	return nil
}

// Stop terminates the monitoring
func (m *Monitor) Stop() {
	if m.detach != nil {
		m.detach()
	}
	close(m.stopCh)
}

func (m *Monitor) run() {
	for {
		select {
		case <-m.stopCh:
			return
		case batch := <-m.eventCh:
			for _, msg := range *batch {
				if err := m.handler(msg); err != nil {
					log.Printf("failed to handle message: %v", err)
				}
			}
		}
	}
}

func AttachMonitor(node pubsub.Host, nodeID pubsub.NodeID, handler MessageHandler) (*Monitor, error) {
	pid := pubsub.PID{
		Node:   nodeID,
		Host:   topology.ControlHost,
		UniqID: fmt.Sprintf("monitor-%d", time.Now().UnixNano()),
	}

	monitor := NewMonitor(node, pid, handler)
	if err := monitor.Start(); err != nil {
		return nil, err
	}

	return monitor, nil
}
