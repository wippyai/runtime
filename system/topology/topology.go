package topology

import (
	"strings"
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
	watchers map[string]bool // PIDs monitoring this process (inbound)
	links    map[string]bool // PIDs linked to this process
	watching map[string]bool // PIDs this process is monitoring (outbound)
}

// Topology implements process monitoring, linking, and lifecycle management.
type Topology struct {
	mu          sync.RWMutex
	processes   map[string]*processState // registered processes by PID string
	router      relay.Receiver           // handles both local and remote routing
	localNodeID relay.NodeID
}

// NewTopology creates a new Topology instance.
func NewTopology(router relay.Receiver, localNodeID relay.NodeID) *Topology {
	return &Topology{
		processes:   make(map[string]*processState),
		router:      router,
		localNodeID: localNodeID,
	}
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
			watching: make(map[string]bool),
		}
	}
	return nil
}

// Wait attaches a caller to monitor a specific pid.
func (t *Topology) Wait(caller, pid relay.PID) error {
	callerKey := caller.String()
	pidKey := pid.String()

	// Check if PID is on remote node.
	if pid.Node != "" && pid.Node != t.localNodeID {
		// Validate caller exists before sending remote request
		t.mu.Lock()
		_, callerExists := t.processes[callerKey]
		if !callerExists {
			t.mu.Unlock()
			return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
				"pid":       callerKey,
				"operation": "monitor",
				"role":      "caller",
			})
		}
		t.mu.Unlock()

		// Send first, then track locally on success
		pkg := topology.MonitorRequest(caller, pid)
		if err := t.router.Send(pkg); err != nil {
			return err
		}

		t.mu.Lock()
		if callerState, exists := t.processes[callerKey]; exists {
			callerState.watching[pidKey] = true
		}
		t.mu.Unlock()
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.processes[pidKey]
	if !exists {
		return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
			"pid":       pidKey,
			"operation": "monitor",
		})
	}

	if state.watchers[callerKey] {
		return topology.ErrAlreadyMonitoring.WithDetails(attrs.Bag{
			"pid":    pidKey,
			"caller": callerKey,
		})
	}

	state.watchers[callerKey] = true

	// Track outbound monitor in caller's state
	if callerState, ok := t.processes[callerKey]; ok {
		callerState.watching[pidKey] = true
	}

	return nil
}

// Release removes a caller's monitoring of a specific pid.
func (t *Topology) Release(caller, pid relay.PID) error {
	// Check if PID is on remote node.
	if pid.Node != "" && pid.Node != t.localNodeID {
		callerKey := caller.String()
		pidKey := pid.String()

		// Send first, then cleanup locally on success
		pkg := topology.MonitorRelease(caller, pid)
		if err := t.router.Send(pkg); err != nil {
			return err
		}

		t.mu.Lock()
		if callerState, exists := t.processes[callerKey]; exists {
			delete(callerState.watching, pidKey)
		}
		t.mu.Unlock()
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	pidKey := pid.String()
	callerKey := caller.String()

	state, exists := t.processes[pidKey]
	if !exists {
		return nil
	}

	delete(state.watchers, callerKey)

	// Clean up caller's watching map
	if callerState, ok := t.processes[callerKey]; ok {
		delete(callerState.watching, pidKey)
	}

	return nil
}

// Link establishes a bidirectional link between two processes.
func (t *Topology) Link(from, to relay.PID) error {
	fromKey := from.String()
	toKey := to.String()

	// Check if to PID is on remote node.
	if to.Node != "" && to.Node != t.localNodeID {
		t.mu.Lock()
		fromState, fromExists := t.processes[fromKey]
		if !fromExists {
			t.mu.Unlock()
			return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
				"pid":       fromKey,
				"operation": "link",
				"role":      "from",
			})
		}
		if fromState.links[toKey] {
			t.mu.Unlock()
			return nil // Already linked.
		}
		t.mu.Unlock()

		// Send first, then track locally on success
		pkg := topology.LinkRequest(from, to)
		if err := t.router.Send(pkg); err != nil {
			return err
		}

		t.mu.Lock()
		if fromState, exists := t.processes[fromKey]; exists {
			fromState.links[toKey] = true
		}
		t.mu.Unlock()
		return nil
	}

	// Local linking.
	t.mu.Lock()
	fromState, fromExists := t.processes[fromKey]
	if !fromExists {
		t.mu.Unlock()
		return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
			"pid":       fromKey,
			"operation": "link",
			"role":      "from",
		})
	}

	toState, toExists := t.processes[toKey]
	if !toExists {
		t.mu.Unlock()
		return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
			"pid":       toKey,
			"operation": "link",
			"role":      "to",
		})
	}

	if fromState.links[toKey] {
		t.mu.Unlock()
		return nil // Already linked.
	}

	fromState.links[toKey] = true
	toState.links[fromKey] = true
	t.mu.Unlock()

	return nil
}

// Unlink removes a bidirectional link between two processes.
func (t *Topology) Unlink(from, to relay.PID) error {
	// Check if to PID is on remote node.
	if to.Node != "" && to.Node != t.localNodeID {
		fromKey := from.String()
		toKey := to.String()

		// Send first, then cleanup locally on success
		pkg := topology.UnlinkRequest(from, to)
		if err := t.router.Send(pkg); err != nil {
			return err
		}

		t.mu.Lock()
		if fromState, exists := t.processes[fromKey]; exists {
			delete(fromState.links, toKey)
		}
		t.mu.Unlock()
		return nil
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
		_ = t.router.Send(pkg)
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
			_ = t.router.Send(pkg)
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

	// Remove this pid from watching maps of processes that watch us.
	for watcherKey := range state.watchers {
		if watcherState, ok := t.processes[watcherKey]; ok {
			delete(watcherState.watching, pidKey)
		}
	}

	delete(t.processes, pidKey)
}

// handleMonitorRequest processes incoming monitor requests from remote nodes.
func (t *Topology) handleMonitorRequest(caller, target relay.PID) error {
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

// handleMonitorRelease processes incoming release requests from remote nodes.
func (t *Topology) handleMonitorRelease(caller, target relay.PID) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.processes[target.String()]
	if !exists {
		return nil
	}

	delete(state.watchers, caller.String())
	return nil
}

// handleLinkRequest processes incoming link requests from remote nodes.
func (t *Topology) handleLinkRequest(from, to relay.PID) error {
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

// handleUnlinkRequest processes incoming unlink requests from remote nodes.
func (t *Topology) handleUnlinkRequest(from, to relay.PID) error {
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

// isWatching checks if caller is watching target (for testing).
func (t *Topology) isWatching(caller, target relay.PID) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	state, exists := t.processes[caller.String()]
	if !exists {
		return false
	}
	return state.watching[target.String()]
}

// HandleNodeExit handles node failure by notifying all local processes
// that were watching or linked to PIDs on the failed node.
func (t *Topology) HandleNodeExit(nodeID relay.NodeID, exitErr error) {
	// Build prefix for fast string matching: "{nodeID@"
	nodePrefix := "{" + nodeID + "@"

	t.mu.RLock()
	type notification struct {
		callerKey string
		targetKey string
	}
	// Pre-allocate with estimated capacity
	toNotify := make([]notification, 0, 64)

	for callerKey, state := range t.processes {
		// Check outbound monitors (watching)
		for targetKey := range state.watching {
			if strings.HasPrefix(targetKey, nodePrefix) {
				toNotify = append(toNotify, notification{callerKey, targetKey})
			}
		}

		// Check links to remote node
		for linkedKey := range state.links {
			if strings.HasPrefix(linkedKey, nodePrefix) {
				toNotify = append(toNotify, notification{callerKey, linkedKey})
			}
		}
	}
	t.mu.RUnlock()

	// Send notifications outside lock - parse PIDs only for matched entries
	for _, n := range toNotify {
		callerPID, err := relay.ParsePID(n.callerKey)
		if err != nil {
			continue
		}
		targetPID, err := relay.ParsePID(n.targetKey)
		if err != nil {
			continue
		}

		linkDownPayload := payload.New(&topology.ExitEvent{
			At:   time.Now(),
			From: targetPID,
			Kind: topology.KindLinkDown,
			Result: &runtime.Result{
				Error: exitErr,
			},
		})
		pkg := relay.NewPackage(
			relay.PID{UniqID: "topology"},
			callerPID,
			topology.TopicEvents,
			linkDownPayload,
		)
		_ = t.router.Send(pkg)
	}

	// Cleanup: remove entries for dead node using prefix match
	t.mu.Lock()

	// Collect PIDs to remove (belonging to dead node)
	var toRemove []string
	for pidKey := range t.processes {
		if strings.HasPrefix(pidKey, nodePrefix) {
			toRemove = append(toRemove, pidKey)
		}
	}

	// Remove dead node PIDs from processes map
	for _, pidKey := range toRemove {
		delete(t.processes, pidKey)
	}

	// Clean up references to dead node in remaining processes
	for _, state := range t.processes {
		for targetKey := range state.watching {
			if strings.HasPrefix(targetKey, nodePrefix) {
				delete(state.watching, targetKey)
			}
		}
		for linkedKey := range state.links {
			if strings.HasPrefix(linkedKey, nodePrefix) {
				delete(state.links, linkedKey)
			}
		}
		for watcherKey := range state.watchers {
			if strings.HasPrefix(watcherKey, nodePrefix) {
				delete(state.watchers, watcherKey)
			}
		}
	}
	t.mu.Unlock()
}

var _ topology.Topology = (*Topology)(nil)
