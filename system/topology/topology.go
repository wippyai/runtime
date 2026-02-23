// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"sync"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
)

const numShards = 32

// processState holds all state for a single registered process.
type processState struct {
	watchers map[string]pid.PID
	links    map[string]pid.PID
	watching map[string]pid.PID
	pid      pid.PID
}

// shard holds a subset of processes with its own lock.
type shard struct {
	processes map[string]*processState
	mu        sync.RWMutex
}

// nodeKeys holds PID keys for a node (wrapped for sync.Map compatibility).
type nodeKeys struct {
	keys []string
	mu   sync.Mutex
}

// Topology implements process monitoring, linking, and lifecycle management.
// Uses sharding to reduce lock contention under high concurrency.
type Topology struct {
	statePool   sync.Pool
	router      relay.Receiver
	nodeIndex   sync.Map
	localNodeID pid.NodeID
	shards      [numShards]shard
}

// NewTopology creates a new Topology instance.
func NewTopology(router relay.Receiver, localNodeID pid.NodeID) *Topology {
	t := &Topology{
		router:      router,
		localNodeID: localNodeID,
	}
	for i := range t.shards {
		t.shards[i].processes = make(map[string]*processState)
	}
	t.statePool.New = func() any {
		return &processState{}
	}
	return t
}

// shardIndex returns the shard index for a given key using inline FNV-1a.
func shardIndex(key string) uint32 {
	const offset32 = 2166136261
	const prime32 = 16777619
	h := uint32(offset32)
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= prime32
	}
	return h & (numShards - 1)
}

// getShard returns the shard for a given key.
func (t *Topology) getShard(key string) *shard {
	return &t.shards[shardIndex(key)]
}

// recycleState clears and returns a processState to the pool.
func (t *Topology) recycleState(s *processState) {
	const maxCap = 16
	if s.watchers != nil {
		if len(s.watchers) > maxCap {
			s.watchers = nil
		} else {
			clear(s.watchers)
		}
	}
	if s.links != nil {
		if len(s.links) > maxCap {
			s.links = nil
		} else {
			clear(s.links)
		}
	}
	if s.watching != nil {
		if len(s.watching) > maxCap {
			s.watching = nil
		} else {
			clear(s.watching)
		}
	}
	t.statePool.Put(s)
}

// Register adds a process ID to the topology.
func (t *Topology) Register(p pid.PID) error {
	key := p.String()
	sh := t.getShard(key)

	sh.mu.Lock()
	defer sh.mu.Unlock()

	if _, exists := sh.processes[key]; exists {
		return topology.ErrPIDAlreadyRegistered
	}

	state := t.statePool.Get().(*processState)
	state.pid = p
	sh.processes[key] = state

	// Track node index for node failure cleanup
	if p.Node != "" {
		t.addToNodeIndex(p.Node, key)
	}

	return nil
}

// addToNodeIndex adds a PID key to the node index.
func (t *Topology) addToNodeIndex(node pid.NodeID, key string) {
	val, _ := t.nodeIndex.LoadOrStore(node, &nodeKeys{})
	nk := val.(*nodeKeys)
	nk.mu.Lock()
	defer nk.mu.Unlock()
	nk.keys = append(nk.keys, key)
}

// removeFromNodeIndex removes a PID key from the node index.
func (t *Topology) removeFromNodeIndex(node pid.NodeID, key string) {
	val, ok := t.nodeIndex.Load(node)
	if !ok {
		return
	}
	nk := val.(*nodeKeys)
	nk.mu.Lock()
	defer nk.mu.Unlock()
	for i, k := range nk.keys {
		if k == key {
			nk.keys = append(nk.keys[:i], nk.keys[i+1:]...)
			break
		}
	}
	if len(nk.keys) == 0 {
		t.nodeIndex.Delete(node)
	}
}

// Monitor attaches a caller to monitor a target pid.
func (t *Topology) Monitor(caller, target pid.PID) error {
	callerKey := caller.String()
	targetKey := target.String()

	// Remote monitoring
	if target.Node != "" && target.Node != t.localNodeID {
		callerSh := t.getShard(callerKey)
		callerSh.mu.Lock()
		if _, exists := callerSh.processes[callerKey]; !exists {
			callerSh.mu.Unlock()
			return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
				"pid":       callerKey,
				"operation": "monitor",
				"role":      "caller",
			})
		}
		callerSh.mu.Unlock()

		pkg := topology.MonitorRequestPackage(caller, target)
		if err := t.router.Send(pkg); err != nil {
			return err
		}

		callerSh.mu.Lock()
		if callerState, exists := callerSh.processes[callerKey]; exists && callerState.pid == caller {
			if callerState.watching == nil {
				callerState.watching = make(map[string]pid.PID)
			}
			callerState.watching[targetKey] = target
		}
		callerSh.mu.Unlock()
		return nil
	}

	// Local monitoring - lock shards in consistent order
	callerIdx := shardIndex(callerKey)
	targetIdx := shardIndex(targetKey)

	if callerIdx == targetIdx {
		return t.monitorSameShard(callerKey, targetKey, caller, target)
	}
	return t.monitorDifferentShards(callerKey, targetKey, caller, target, callerIdx, targetIdx)
}

func (t *Topology) monitorSameShard(callerKey, targetKey string, caller, target pid.PID) error {
	sh := t.getShard(targetKey)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	targetState, exists := sh.processes[targetKey]
	if !exists {
		return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
			"pid":       targetKey,
			"operation": "monitor",
		})
	}

	if targetState.watchers != nil {
		if _, already := targetState.watchers[callerKey]; already {
			return topology.ErrAlreadyMonitoring
		}
	}

	if targetState.watchers == nil {
		targetState.watchers = make(map[string]pid.PID)
	}
	targetState.watchers[callerKey] = caller

	if callerState, ok := sh.processes[callerKey]; ok {
		if callerState.watching == nil {
			callerState.watching = make(map[string]pid.PID)
		}
		callerState.watching[targetKey] = target
	}

	return nil
}

func (t *Topology) monitorDifferentShards(callerKey, targetKey string, caller, target pid.PID, callerIdx, targetIdx uint32) error {
	// Lock in consistent order to prevent deadlock
	first, second := &t.shards[callerIdx], &t.shards[targetIdx]
	if callerIdx > targetIdx {
		first, second = second, first
	}

	first.mu.Lock()
	second.mu.Lock()
	defer first.mu.Unlock()
	defer second.mu.Unlock()

	targetSh := &t.shards[targetIdx]
	targetState, exists := targetSh.processes[targetKey]
	if !exists {
		return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
			"pid":       targetKey,
			"operation": "monitor",
		})
	}

	if targetState.watchers != nil {
		if _, already := targetState.watchers[callerKey]; already {
			return topology.ErrAlreadyMonitoring
		}
	}

	if targetState.watchers == nil {
		targetState.watchers = make(map[string]pid.PID)
	}
	targetState.watchers[callerKey] = caller

	callerSh := &t.shards[callerIdx]
	if callerState, ok := callerSh.processes[callerKey]; ok {
		if callerState.watching == nil {
			callerState.watching = make(map[string]pid.PID)
		}
		callerState.watching[targetKey] = target
	}

	return nil
}

// Demonitor removes a caller's monitoring of a target pid.
func (t *Topology) Demonitor(caller, target pid.PID) error {
	callerKey := caller.String()
	targetKey := target.String()

	// Remote demonitoring
	if target.Node != "" && target.Node != t.localNodeID {
		pkg := topology.MonitorReleasePackage(caller, target)
		if err := t.router.Send(pkg); err != nil {
			return err
		}

		callerSh := t.getShard(callerKey)
		callerSh.mu.Lock()
		if callerState, exists := callerSh.processes[callerKey]; exists {
			delete(callerState.watching, targetKey)
		}
		callerSh.mu.Unlock()
		return nil
	}

	// Local demonitoring - lock shards in consistent order to prevent race conditions
	callerIdx := shardIndex(callerKey)
	targetIdx := shardIndex(targetKey)

	if callerIdx == targetIdx {
		sh := t.getShard(targetKey)
		sh.mu.Lock()
		defer sh.mu.Unlock()

		if targetState, exists := sh.processes[targetKey]; exists {
			delete(targetState.watchers, callerKey)
		}
		if callerState, exists := sh.processes[callerKey]; exists {
			delete(callerState.watching, targetKey)
		}
		return nil
	}

	first, second := &t.shards[callerIdx], &t.shards[targetIdx]
	if callerIdx > targetIdx {
		first, second = second, first
	}

	first.mu.Lock()
	second.mu.Lock()
	defer first.mu.Unlock()
	defer second.mu.Unlock()

	targetSh := &t.shards[targetIdx]
	if targetState, exists := targetSh.processes[targetKey]; exists {
		delete(targetState.watchers, callerKey)
	}

	callerSh := &t.shards[callerIdx]
	if callerState, exists := callerSh.processes[callerKey]; exists {
		delete(callerState.watching, targetKey)
	}

	return nil
}

// Link establishes a bidirectional link between two processes.
func (t *Topology) Link(from, to pid.PID) error {
	fromKey := from.String()
	toKey := to.String()

	// Remote linking
	if to.Node != "" && to.Node != t.localNodeID {
		fromSh := t.getShard(fromKey)
		fromSh.mu.Lock()
		fromState, exists := fromSh.processes[fromKey]
		if !exists {
			fromSh.mu.Unlock()
			return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
				"pid":       fromKey,
				"operation": "link",
				"role":      "from",
			})
		}
		if fromState.links != nil {
			if _, already := fromState.links[toKey]; already {
				fromSh.mu.Unlock()
				return nil
			}
		}
		fromSh.mu.Unlock()

		pkg := topology.LinkRequestPackage(from, to)
		if err := t.router.Send(pkg); err != nil {
			return err
		}

		fromSh.mu.Lock()
		if fromState, exists := fromSh.processes[fromKey]; exists && fromState.pid == from {
			if fromState.links == nil {
				fromState.links = make(map[string]pid.PID)
			}
			fromState.links[toKey] = to
		}
		fromSh.mu.Unlock()
		return nil
	}

	// Local linking - lock shards in consistent order
	fromIdx := shardIndex(fromKey)
	toIdx := shardIndex(toKey)

	if fromIdx == toIdx {
		return t.linkSameShard(fromKey, toKey, from, to)
	}
	return t.linkDifferentShards(fromKey, toKey, from, to, fromIdx, toIdx)
}

func (t *Topology) linkSameShard(fromKey, toKey string, from, to pid.PID) error {
	sh := t.getShard(fromKey)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	fromState, exists := sh.processes[fromKey]
	if !exists {
		return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
			"pid":       fromKey,
			"operation": "link",
			"role":      "from",
		})
	}

	toState, exists := sh.processes[toKey]
	if !exists {
		return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
			"pid":       toKey,
			"operation": "link",
			"role":      "to",
		})
	}

	if fromState.links != nil {
		if _, already := fromState.links[toKey]; already {
			return nil
		}
	}

	if fromState.links == nil {
		fromState.links = make(map[string]pid.PID)
	}
	if toState.links == nil {
		toState.links = make(map[string]pid.PID)
	}
	fromState.links[toKey] = to
	toState.links[fromKey] = from

	return nil
}

func (t *Topology) linkDifferentShards(fromKey, toKey string, from, to pid.PID, fromIdx, toIdx uint32) error {
	first, second := &t.shards[fromIdx], &t.shards[toIdx]
	if fromIdx > toIdx {
		first, second = second, first
	}

	first.mu.Lock()
	second.mu.Lock()
	defer first.mu.Unlock()
	defer second.mu.Unlock()

	fromSh := &t.shards[fromIdx]
	fromState, exists := fromSh.processes[fromKey]
	if !exists {
		return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
			"pid":       fromKey,
			"operation": "link",
			"role":      "from",
		})
	}

	toSh := &t.shards[toIdx]
	toState, exists := toSh.processes[toKey]
	if !exists {
		return topology.ErrPIDNotRegistered.WithDetails(attrs.Bag{
			"pid":       toKey,
			"operation": "link",
			"role":      "to",
		})
	}

	if fromState.links != nil {
		if _, already := fromState.links[toKey]; already {
			return nil
		}
	}

	if fromState.links == nil {
		fromState.links = make(map[string]pid.PID)
	}
	if toState.links == nil {
		toState.links = make(map[string]pid.PID)
	}
	fromState.links[toKey] = to
	toState.links[fromKey] = from

	return nil
}

// Unlink removes a bidirectional link between two processes.
func (t *Topology) Unlink(from, to pid.PID) error {
	fromKey := from.String()
	toKey := to.String()

	// Remote unlinking
	if to.Node != "" && to.Node != t.localNodeID {
		pkg := topology.UnlinkRequestPackage(from, to)
		if err := t.router.Send(pkg); err != nil {
			return err
		}

		fromSh := t.getShard(fromKey)
		fromSh.mu.Lock()
		if fromState, exists := fromSh.processes[fromKey]; exists {
			delete(fromState.links, toKey)
		}
		fromSh.mu.Unlock()
		return nil
	}

	// Local unlinking - lock shards in consistent order to prevent race conditions
	fromIdx := shardIndex(fromKey)
	toIdx := shardIndex(toKey)

	if fromIdx == toIdx {
		sh := t.getShard(fromKey)
		sh.mu.Lock()
		defer sh.mu.Unlock()

		if fromState, exists := sh.processes[fromKey]; exists {
			delete(fromState.links, toKey)
		}
		if toState, exists := sh.processes[toKey]; exists {
			delete(toState.links, fromKey)
		}
		return nil
	}

	first, second := &t.shards[fromIdx], &t.shards[toIdx]
	if fromIdx > toIdx {
		first, second = second, first
	}

	first.mu.Lock()
	second.mu.Lock()
	defer first.mu.Unlock()
	defer second.mu.Unlock()

	fromSh := &t.shards[fromIdx]
	if fromState, exists := fromSh.processes[fromKey]; exists {
		delete(fromState.links, toKey)
	}

	toSh := &t.shards[toIdx]
	if toState, exists := toSh.processes[toKey]; exists {
		delete(toState.links, fromKey)
	}

	return nil
}

// GetLinks returns all processes linked to the given pid.
func (t *Topology) GetLinks(p pid.PID) []pid.PID {
	key := p.String()
	sh := t.getShard(key)

	sh.mu.RLock()
	defer sh.mu.RUnlock()

	state, exists := sh.processes[key]
	if !exists || state.links == nil {
		return nil
	}

	result := make([]pid.PID, 0, len(state.links))
	for _, linkedPID := range state.links {
		result = append(result, linkedPID)
	}
	return result
}

// Complete notifies watchers/links and removes the pid in one operation.
func (t *Topology) Complete(p pid.PID, result *runtime.Result) {
	key := p.String()
	sh := t.getShard(key)

	sh.mu.Lock()
	state, exists := sh.processes[key]
	if !exists {
		sh.mu.Unlock()
		return
	}

	hasWatchers := len(state.watchers) > 0
	hasLinks := result.Error != nil && len(state.links) > 0

	var watchers []pid.PID
	if hasWatchers {
		watchers = make([]pid.PID, 0, len(state.watchers))
		for _, wPID := range state.watchers {
			watchers = append(watchers, wPID)
		}
	}

	var linkedPIDs []pid.PID
	if hasLinks {
		linkedPIDs = make([]pid.PID, 0, len(state.links))
		for _, lPID := range state.links {
			linkedPIDs = append(linkedPIDs, lPID)
		}
	}

	// Collect remote targets this process was watching
	var remoteWatching []pid.PID
	for _, targetPID := range state.watching {
		if targetPID.Node != "" && targetPID.Node != t.localNodeID {
			remoteWatching = append(remoteWatching, targetPID)
		}
	}

	delete(sh.processes, key)
	sh.mu.Unlock()

	// Clean up cross-references in other shards
	t.cleanupReferences(key, state)

	// Clean up node index
	if p.Node != "" {
		t.removeFromNodeIndex(p.Node, key)
	}

	// Recycle state
	t.recycleState(state)

	// Send notifications
	if len(watchers) > 0 {
		exitPayload := payload.New(&topology.ExitEvent{
			At:     time.Now(),
			From:   p,
			Kind:   topology.Exit,
			Result: result,
		})
		for _, wPID := range watchers {
			pkg := relay.NewPackage(p, wPID, topology.TopicEvents, exitPayload)
			_ = t.router.Send(pkg)
		}
	}

	if len(linkedPIDs) > 0 {
		linkDownPayload := payload.New(&topology.ExitEvent{
			At:     time.Now(),
			From:   p,
			Kind:   topology.LinkDown,
			Result: result,
		})
		for _, linkedPID := range linkedPIDs {
			pkg := relay.NewPackage(topology.SystemPID, linkedPID, topology.TopicEvents, linkDownPayload)
			_ = t.router.Send(pkg)
		}
	}

	// Release monitors on remote targets
	for _, targetPID := range remoteWatching {
		pkg := topology.MonitorReleasePackage(p, targetPID)
		_ = t.router.Send(pkg)
	}
}

// cleanupReferences removes this PID from other processes' links/watching maps.
func (t *Topology) cleanupReferences(key string, state *processState) {
	// Remove from linked processes
	for linkedKey := range state.links {
		linkedSh := t.getShard(linkedKey)
		linkedSh.mu.Lock()
		if linkedState, ok := linkedSh.processes[linkedKey]; ok {
			delete(linkedState.links, key)
		}
		linkedSh.mu.Unlock()
	}

	// Remove from watchers' watching maps
	for watcherKey := range state.watchers {
		watcherSh := t.getShard(watcherKey)
		watcherSh.mu.Lock()
		if watcherState, ok := watcherSh.processes[watcherKey]; ok {
			delete(watcherState.watching, key)
		}
		watcherSh.mu.Unlock()
	}
}

// Remove completely removes a pid from topology.
func (t *Topology) Remove(p pid.PID) {
	key := p.String()
	sh := t.getShard(key)

	sh.mu.Lock()
	state, exists := sh.processes[key]
	if !exists {
		sh.mu.Unlock()
		return
	}

	delete(sh.processes, key)
	sh.mu.Unlock()

	t.cleanupReferences(key, state)

	if p.Node != "" {
		t.removeFromNodeIndex(p.Node, key)
	}

	t.recycleState(state)
}

// HandleNodeExit handles node failure by notifying all local processes
// that were watching or linked to PIDs on the failed node.
func (t *Topology) HandleNodeExit(nodeID pid.NodeID, exitErr error) {
	// Get PIDs on the failed node (may be empty if we only had watchers)
	var deadPIDKeys []string
	deadKeySet := make(map[string]bool)

	if val, ok := t.nodeIndex.Load(nodeID); ok {
		nk := val.(*nodeKeys)
		nk.mu.Lock()
		deadPIDKeys = make([]string, len(nk.keys))
		copy(deadPIDKeys, nk.keys)
		nk.mu.Unlock()

		for _, key := range deadPIDKeys {
			deadKeySet[key] = true
		}
	}

	type notification struct {
		caller pid.PID
		target pid.PID
	}
	var toNotify []notification

	// Scan all shards for processes watching/linked to dead node
	for i := range t.shards {
		sh := &t.shards[i]
		sh.mu.RLock()
		for _, state := range sh.processes {
			for targetKey, targetPID := range state.watching {
				if deadKeySet[targetKey] || targetPID.Node == nodeID {
					toNotify = append(toNotify, notification{state.pid, targetPID})
				}
			}
			for linkedKey, linkedPID := range state.links {
				if deadKeySet[linkedKey] || linkedPID.Node == nodeID {
					toNotify = append(toNotify, notification{state.pid, linkedPID})
				}
			}
		}
		sh.mu.RUnlock()
	}

	// Send notifications
	for _, n := range toNotify {
		linkDownPayload := payload.New(&topology.ExitEvent{
			At:   time.Now(),
			From: n.target,
			Kind: topology.LinkDown,
			Result: &runtime.Result{
				Error: exitErr,
			},
		})
		pkg := relay.NewPackage(topology.SystemPID, n.caller, topology.TopicEvents, linkDownPayload)
		_ = t.router.Send(pkg)
	}

	// Cleanup: remove dead node PIDs from all shards
	for _, pidKey := range deadPIDKeys {
		sh := t.getShard(pidKey)
		sh.mu.Lock()
		if state, exists := sh.processes[pidKey]; exists {
			delete(sh.processes, pidKey)
			t.recycleState(state)
		}
		sh.mu.Unlock()
	}
	t.nodeIndex.Delete(nodeID)

	// Clean up references in remaining processes
	for i := range t.shards {
		sh := &t.shards[i]
		sh.mu.Lock()
		for _, state := range sh.processes {
			for targetKey, targetPID := range state.watching {
				if deadKeySet[targetKey] || targetPID.Node == nodeID {
					delete(state.watching, targetKey)
				}
			}
			for linkedKey, linkedPID := range state.links {
				if deadKeySet[linkedKey] || linkedPID.Node == nodeID {
					delete(state.links, linkedKey)
				}
			}
			for watcherKey, watcherPID := range state.watchers {
				if deadKeySet[watcherKey] || watcherPID.Node == nodeID {
					delete(state.watchers, watcherKey)
				}
			}
		}
		sh.mu.Unlock()
	}
}

var _ topology.Topology = (*Topology)(nil)
