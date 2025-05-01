package topology

import (
	"fmt"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/topology"
)

// Topology implements process monitoring, linking, and lifecycle management
// capabilities for the Wippy runtime. It maintains process relationships
// including registrations, monitors, and links between processes.
type Topology struct {
	monitors sync.Map // map[string]*sync.Map - watchers for each pid
	registry sync.Map // map[string]bool - registered PIDs
	links    sync.Map // map[string]*sync.Map - bidirectional links between PIDs
	upstream pubsub.Receiver
}

// NewTopology creates a new Topology instance with the given context and upstream receiver.
// The returned Topology implements the topology.Topology interface.
func NewTopology(upstream pubsub.Receiver) *Topology {
	return &Topology{
		upstream: upstream,
	}
}

// Send forwards a package to the upstream receiver.
func (t *Topology) Send(pkg *pubsub.Package) error {
	return t.upstream.Send(pkg)
}

// Register adds a process ID to the registry, allowing it to be monitored
// and linked with other processes.
func (t *Topology) Register(pid pubsub.PID) error {
	t.registry.LoadOrStore(pid.String(), true)
	return nil
}

// Wait attaches a caller to monitor a specific pid.
// Returns error if pid is not registered or already being monitored by caller.
func (t *Topology) Wait(caller, pid pubsub.PID) error {
	// Check if pid is registered
	if _, ok := t.registry.Load(pid.String()); !ok {
		return fmt.Errorf("cannot monitor unregistered pid: %s", pid)
	}

	value, _ := t.monitors.LoadOrStore(pid.String(), &sync.Map{})
	watchers := value.(*sync.Map)

	_, loaded := watchers.LoadOrStore(caller.String(), true)
	if loaded {
		return fmt.Errorf("already monitoring pid: %s", pid)
	}

	return nil
}

// Release removes a caller's monitoring of a specific pid.
// Returns nil if the operation is successful or if the pid is not being monitored.
func (t *Topology) Release(caller, pid pubsub.PID) error {
	value, ok := t.monitors.Load(pid.String())
	if !ok {
		return nil
	}
	watchers := value.(*sync.Map)

	watchers.Delete(caller.String())

	empty := true
	watchers.Range(func(_, _ interface{}) bool {
		empty = false
		return false
	})
	if empty {
		t.monitors.Delete(pid.String())
	}

	return nil
}

// Link establishes a bidirectional link between two processes.
// Both processes must be registered first.
// Returns error if either process is not registered.
func (t *Topology) Link(from, to pubsub.PID) error {
	// Verify both PIDs are registered
	if _, ok := t.registry.Load(from.String()); !ok {
		return fmt.Errorf("cannot link unregistered pid: %s", from)
	}
	if _, ok := t.registry.Load(to.String()); !ok {
		return fmt.Errorf("cannot link unregistered pid: %s", to)
	}

	// Create or get links maps for both processes
	fromLinksValue, _ := t.links.LoadOrStore(from.String(), &sync.Map{})
	fromLinks := fromLinksValue.(*sync.Map)

	toLinksValue, _ := t.links.LoadOrStore(to.String(), &sync.Map{})
	toLinks := toLinksValue.(*sync.Map)

	// Check if already linked (don't send duplicate notifications)
	_, alreadyLinked := fromLinks.Load(to.String())
	if alreadyLinked {
		return nil
	}

	// Create bidirectional links
	fromLinks.Store(to.String(), true)
	toLinks.Store(from.String(), true)

	return nil
}

// Unlink removes a bidirectional link between two processes.
// Returns nil if the operation is successful or if the processes are not linked.
func (t *Topology) Unlink(from, to pubsub.PID) error {
	// Check if links exist
	fromLinksValue, fromOk := t.links.Load(from.String())
	if !fromOk {
		return nil // No links for 'from'
	}

	fromLinks := fromLinksValue.(*sync.Map)
	_, linked := fromLinks.Load(to.String())
	if !linked {
		return nil // Not linked
	}

	// Done bidirectional links
	fromLinks.Delete(to.String())

	if toLinksValue, ok := t.links.Load(to.String()); ok {
		toLinks := toLinksValue.(*sync.Map)
		toLinks.Delete(from.String())
	}

	return nil
}

// GetLinks returns all processes linked to the given pid.
// Returns an empty slice if the pid has no links.
func (t *Topology) GetLinks(pid pubsub.PID) []pubsub.PID {
	var linkedPIDs []pubsub.PID

	linksValue, ok := t.links.Load(pid.String())
	if !ok {
		return linkedPIDs
	}

	links := linksValue.(*sync.Map)
	links.Range(func(key, _ interface{}) bool {
		linkedPIDStr, ok := key.(string)
		if !ok {
			return true
		}

		linkedPID, err := pubsub.ParsePID(linkedPIDStr)
		if err != nil {
			return true
		}

		linkedPIDs = append(linkedPIDs, linkedPID)
		return true
	})

	return linkedPIDs
}

// Notify sends exit event to all watchers and links of a pid.
// The provided result contains the process exit information to be shared.
func (t *Topology) Notify(pid pubsub.PID, result *runtime.Result) {
	// Send to all monitors
	if value, ok := t.monitors.Load(pid.String()); ok {
		resultPayload := payload.New(&topology.ExitEvent{
			At:     time.Now(),
			From:   pid,
			Kind:   topology.KindExit,
			Result: result,
		})

		watchers := value.(*sync.Map)
		watchers.Range(func(key, _ interface{}) bool {
			watcherPID, ok := key.(string)
			if !ok {
				return true
			}

			callerPID, err := pubsub.ParsePID(watcherPID)
			if err != nil {
				return true
			}

			pkg := pubsub.NewPackage(
				pubsub.PID{UniqID: "topology"},
				callerPID,
				topology.TopicEvents,
				resultPayload,
			)

			_ = t.upstream.Send(pkg)
			return true
		})
	}

	// Check if this is a normal exit
	isNormalExit := result.Error == nil

	// For linked processes, only send KindLinkDown for abnormal exits
	linkedPIDs := t.GetLinks(pid)
	if len(linkedPIDs) > 0 && !isNormalExit {
		exitPayload := payload.New(&topology.ExitEvent{
			At:     time.Now(),
			From:   pid,
			Kind:   topology.KindLinkDown,
			Result: result,
		})

		for _, linkedPID := range linkedPIDs {
			pkg := pubsub.NewPackage(
				pubsub.PID{UniqID: "topology"},
				linkedPID,
				topology.TopicEvents,
				exitPayload,
			)
			_ = t.upstream.Send(pkg)
		}
	}

	// Cleanup is important regardless of exit type
	// This is normally done by calling t.Remove(pid) after Notify
	// but we can ensure the cleanup is done here as well

	// Note: We don't do the actual cleanup in Notify to allow
	// separate control over notification and removal timing.
	// The caller should call t.Remove(pid) after this method.
}

// Remove completely removes a pid and all its watchers, destroying all links.
// This should be called when a process terminates to clean up all its references.
func (t *Topology) Remove(pid pubsub.PID) {
	// Get linked PIDs before removing them
	linkedPIDs := t.GetLinks(pid)

	// Done all links
	for _, linkedPID := range linkedPIDs {
		_ = t.Unlink(pid, linkedPID)
	}

	// Done from monitors
	t.monitors.Delete(pid.String())

	// Done from links
	t.links.Delete(pid.String())

	// Done from registry
	t.registry.Delete(pid.String())
}

// Ensure Registry implements the operation.Registry interface
var _ topology.Topology = (*Topology)(nil)
