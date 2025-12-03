package topology

import (
	"sync"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
)

// processState holds all state for a single registered process.
type processState struct {
	watchers map[string]bool // PIDs monitoring this process
	links    map[string]bool // PIDs linked to this process
}

// Topology implements process monitoring, linking, and lifecycle management.
type Topology struct {
	mu          sync.RWMutex
	processes   map[string]*processState // registered processes by PID string
	upstream    relay.Receiver
	router      relay.Receiver
	localNodeID relay.NodeID
}

// NewTopology creates a new Topology instance.
func NewTopology(upstream relay.Receiver, router relay.Receiver, localNodeID relay.NodeID) *Topology {
	return &Topology{
		processes:   make(map[string]*processState),
		upstream:    upstream,
		router:      router,
		localNodeID: localNodeID,
	}
}

// Send forwards a package to the upstream receiver.
func (t *Topology) Send(pkg *relay.Package) error {
	return t.upstream.Send(pkg)
}

// Register adds a process ID to the registry.
func (t *Topology) Register(pid relay.PID) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := pid.String()
	if _, exists := t.processes[key]; !exists {
		t.processes[key] = &processState{
			watchers: make(map[string]bool),
			links:    make(map[string]bool),
		}
	}
	return nil
}

// Wait attaches a caller to monitor a specific pid.
func (t *Topology) Wait(caller, pid relay.PID) error {
	// Check if PID is on remote node.
	if pid.Node != "" && pid.Node != t.localNodeID {
		pkg := topology.MonitorRequest(caller, pid)
		return t.router.Send(pkg)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.processes[pid.String()]
	if !exists {
		return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
			"pid":       pid.String(),
			"operation": "monitor",
		})
	}

	callerKey := caller.String()
	if state.watchers[callerKey] {
		return topology.ErrAlreadyMonitoring.WithDetails(attrs.Bag{
			"pid":    pid.String(),
			"caller": callerKey,
		})
	}

	state.watchers[callerKey] = true
	return nil
}

// Release removes a caller's monitoring of a specific pid.
func (t *Topology) Release(caller, pid relay.PID) error {
	// Check if PID is on remote node.
	if pid.Node != "" && pid.Node != t.localNodeID {
		pkg := topology.MonitorRelease(caller, pid)
		return t.router.Send(pkg)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.processes[pid.String()]
	if !exists {
		return nil
	}

	delete(state.watchers, caller.String())
	return nil
}

// Link establishes a bidirectional link between two processes.
func (t *Topology) Link(from, to relay.PID) error {
	t.mu.Lock()

	fromState, fromExists := t.processes[from.String()]
	if !fromExists {
		t.mu.Unlock()
		return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
			"pid":       from.String(),
			"operation": "link",
			"role":      "from",
		})
	}

	// Check if to PID is on remote node.
	if to.Node != "" && to.Node != t.localNodeID {
		toKey := to.String()
		if fromState.links[toKey] {
			t.mu.Unlock()
			return nil // Already linked.
		}
		fromState.links[toKey] = true
		t.mu.Unlock()

		pkg := topology.LinkRequest(from, to)
		return t.router.Send(pkg)
	}

	// Local linking.
	toState, toExists := t.processes[to.String()]
	if !toExists {
		t.mu.Unlock()
		return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
			"pid":       to.String(),
			"operation": "link",
			"role":      "to",
		})
	}

	fromKey := from.String()
	toKey := to.String()

	if fromState.links[toKey] {
		t.mu.Unlock()
		return nil // Already linked.
	}

	// Bidirectional link.
	fromState.links[toKey] = true
	toState.links[fromKey] = true
	t.mu.Unlock()

	return nil
}

// Unlink removes a bidirectional link between two processes.
func (t *Topology) Unlink(from, to relay.PID) error {
	// Check if to PID is on remote node.
	if to.Node != "" && to.Node != t.localNodeID {
		t.mu.Lock()
		if fromState, exists := t.processes[from.String()]; exists {
			delete(fromState.links, to.String())
		}
		t.mu.Unlock()

		pkg := topology.UnlinkRequest(from, to)
		return t.router.Send(pkg)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	fromKey := from.String()
	toKey := to.String()

	if fromState, exists := t.processes[fromKey]; exists {
		delete(fromState.links, toKey)
	}

	if toState, exists := t.processes[toKey]; exists {
		delete(toState.links, fromKey)
	}

	return nil
}

// GetLinks returns all processes linked to the given pid.
func (t *Topology) GetLinks(pid relay.PID) []relay.PID {
	t.mu.RLock()
	defer t.mu.RUnlock()

	state, exists := t.processes[pid.String()]
	if !exists {
		return nil
	}

	result := make([]relay.PID, 0, len(state.links))
	for pidStr := range state.links {
		linkedPID, err := relay.ParsePID(pidStr)
		if err == nil {
			result = append(result, linkedPID)
		}
	}
	return result
}

// Notify sends exit event to all watchers and links of a pid.
func (t *Topology) Notify(pid relay.PID, result *runtime.Result) {
	t.mu.RLock()
	state, exists := t.processes[pid.String()]
	if !exists {
		t.mu.RUnlock()
		return
	}

	// Copy watchers and links to avoid holding lock during sends.
	watchers := make([]string, 0, len(state.watchers))
	for w := range state.watchers {
		watchers = append(watchers, w)
	}

	var linkedPIDs []string
	if result.Error != nil {
		linkedPIDs = make([]string, 0, len(state.links))
		for l := range state.links {
			linkedPIDs = append(linkedPIDs, l)
		}
	}
	t.mu.RUnlock()

	// Send exit events to watchers.
	exitPayload := payload.New(&topology.ExitEvent{
		At:     time.Now(),
		From:   pid,
		Kind:   topology.KindExit,
		Result: result,
	})

	for _, watcherStr := range watchers {
		wPID, err := relay.ParsePID(watcherStr)
		if err != nil {
			continue
		}
		pkg := relay.NewPackage(pid, wPID, topology.TopicEvents, exitPayload)
		_ = t.upstream.Send(pkg)
	}

	// Send link-down to linked processes for abnormal exits.
	if len(linkedPIDs) > 0 {
		linkDownPayload := payload.New(&topology.ExitEvent{
			At:     time.Now(),
			From:   pid,
			Kind:   topology.KindLinkDown,
			Result: result,
		})

		for _, linkedStr := range linkedPIDs {
			linkedPID, err := relay.ParsePID(linkedStr)
			if err != nil {
				continue
			}
			pkg := relay.NewPackage(
				relay.PID{UniqID: "topology"},
				linkedPID,
				topology.TopicEvents,
				linkDownPayload,
			)
			_ = t.upstream.Send(pkg)
		}
	}
}

// Remove completely removes a pid and all its watchers and links.
func (t *Topology) Remove(pid relay.PID) {
	t.mu.Lock()
	defer t.mu.Unlock()

	pidKey := pid.String()
	state, exists := t.processes[pidKey]
	if !exists {
		return
	}

	// Remove this pid from all linked processes.
	for linkedKey := range state.links {
		if linkedState, ok := t.processes[linkedKey]; ok {
			delete(linkedState.links, pidKey)
		}
	}

	delete(t.processes, pidKey)
}

// HandleMonitorRequest processes incoming monitor requests from remote nodes.
func (t *Topology) HandleMonitorRequest(caller, target relay.PID) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.processes[target.String()]
	if !exists {
		return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
			"pid":       target.String(),
			"operation": "monitor",
			"caller":    caller.String(),
		})
	}

	state.watchers[caller.String()] = true
	return nil
}

// HandleMonitorRelease processes incoming release requests from remote nodes.
func (t *Topology) HandleMonitorRelease(caller, target relay.PID) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.processes[target.String()]
	if !exists {
		return nil
	}

	delete(state.watchers, caller.String())
	return nil
}

// HandleLinkRequest processes incoming link requests from remote nodes.
func (t *Topology) HandleLinkRequest(from, to relay.PID) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.processes[to.String()]
	if !exists {
		return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
			"pid":       to.String(),
			"operation": "link",
			"from":      from.String(),
		})
	}

	state.links[from.String()] = true
	return nil
}

// HandleUnlinkRequest processes incoming unlink requests from remote nodes.
func (t *Topology) HandleUnlinkRequest(from, to relay.PID) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.processes[to.String()]
	if !exists {
		return nil
	}

	delete(state.links, from.String())
	return nil
}

// hasWatcher checks if a caller is watching a pid (for testing).
func (t *Topology) hasWatcher(pid, caller relay.PID) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	state, exists := t.processes[pid.String()]
	if !exists {
		return false
	}
	return state.watchers[caller.String()]
}

// watcherCount returns the number of watchers for a pid (for testing).
func (t *Topology) watcherCount(pid relay.PID) int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	state, exists := t.processes[pid.String()]
	if !exists {
		return 0
	}
	return len(state.watchers)
}

var _ topology.Topology = (*Topology)(nil)
