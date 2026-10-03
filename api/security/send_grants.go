// SPDX-License-Identifier: MPL-2.0

package security

import (
	"context"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
)

// sendGrantsKey holds a restricted process's send grants. Frames forked from
// it, including child processes, inherit a copy: a process spawned by a
// restricted process is restricted to what its spawner could address. As an
// execution value, the grants are shared by every code incarnation of the
// process.
var sendGrantsKey = &ctxapi.Key{Name: "security.process_send_grants", Inherit: true, Execution: true}

// ProcessSendGrants is the set of PIDs a capability-restricted process may
// address besides its parent. It contains only PIDs the runtime handed to the
// process (itself, processes it spawned, senders of messages it received,
// registry lookup results) or its spawner held, so a PID string the process
// made up is never in it. A process acquires grants from its execution while
// other goroutines fork its frame, which copies them, so access is
// synchronized.
type ProcessSendGrants struct {
	pids map[grantKey]struct{}
	mu   sync.RWMutex
}

type grantKey struct {
	node pid.NodeID
	host pid.HostID
	uniq string
}

// NewProcessSendGrants returns grants holding the seed PIDs.
func NewProcessSendGrants(seed ...pid.PID) *ProcessSendGrants {
	g := &ProcessSendGrants{pids: make(map[grantKey]struct{}, len(seed)+4)}
	for _, p := range seed {
		g.Grant(p)
	}
	return g
}

// Grant adds p to the set.
func (g *ProcessSendGrants) Grant(p pid.PID) {
	if p.UniqID == "" {
		return
	}
	g.mu.Lock()
	g.pids[keyOf(p)] = struct{}{}
	g.mu.Unlock()
}

// Holds reports whether p was granted.
func (g *ProcessSendGrants) Holds(p pid.PID) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, ok := g.pids[keyOf(p)]
	return ok
}

// Clone implements ctxapi.Cloner: a forked frame gets an independent copy,
// so acquisitions after the fork stay with the process that made them.
func (g *ProcessSendGrants) Clone() any {
	g.mu.RLock()
	defer g.mu.RUnlock()
	c := &ProcessSendGrants{pids: make(map[grantKey]struct{}, len(g.pids))}
	for k := range g.pids {
		c.pids[k] = struct{}{}
	}
	return c
}

func keyOf(p pid.PID) grantKey {
	return grantKey{node: p.Node, host: p.Host, uniq: p.UniqID}
}

// ProcessSendGrantsPair installs grants on a process frame, making the
// process capability-restricted.
func ProcessSendGrantsPair(grants *ProcessSendGrants) ctxapi.Pair {
	return ctxapi.Pair{Key: sendGrantsKey, Value: grants}
}

// GetProcessSendGrants returns the grants of a capability-restricted
// process, or nil for an unrestricted one.
func GetProcessSendGrants(ctx context.Context) *ProcessSendGrants {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(sendGrantsKey); ok {
		if grants, ok := val.(*ProcessSendGrants); ok {
			return grants
		}
	}
	return nil
}

// GrantProcessSend records that the runtime handed p to the process in ctx.
// It does nothing for unrestricted processes.
func GrantProcessSend(ctx context.Context, p pid.PID) {
	if grants := GetProcessSendGrants(ctx); grants != nil {
		grants.Grant(p)
	}
}
