// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	apiprocess "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

type relationshipHost struct {
	pid            pid.PID
	terminateCount int
	mu             sync.Mutex
}

func (h *relationshipHost) Run(context.Context, *apiprocess.Start) (pid.PID, error) {
	return h.pid, nil
}
func (h *relationshipHost) Send(*relay.Package) error { return nil }
func (h *relationshipHost) Terminate(context.Context, pid.PID) error {
	h.mu.Lock()
	h.terminateCount++
	h.mu.Unlock()
	return nil
}

func (h *relationshipHost) terminations() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.terminateCount
}

type relationshipTopology struct {
	monitorErr error
	linkErr    error
	monitored  map[string]struct{}
	linked     map[string]struct{}
}

func relationshipKey(from, to pid.PID) string { return from.String() + "->" + to.String() }
func (t *relationshipTopology) Monitor(from, to pid.PID) error {
	if t.monitorErr != nil {
		return t.monitorErr
	}
	t.monitored[relationshipKey(from, to)] = struct{}{}
	return nil
}
func (t *relationshipTopology) Demonitor(from, to pid.PID) error {
	delete(t.monitored, relationshipKey(from, to))
	return nil
}
func (t *relationshipTopology) Link(from, to pid.PID) error {
	if t.linkErr != nil {
		return t.linkErr
	}
	t.linked[relationshipKey(from, to)] = struct{}{}
	return nil
}
func (t *relationshipTopology) Unlink(from, to pid.PID) error {
	delete(t.linked, relationshipKey(from, to))
	return nil
}
func (t *relationshipTopology) GetLinks(pid.PID) []pid.PID        { return nil }
func (t *relationshipTopology) Register(pid.PID) error            { return nil }
func (t *relationshipTopology) Complete(pid.PID, *runtime.Result) {}
func (t *relationshipTopology) Remove(pid.PID)                    {}

func relationshipStart(parent pid.PID) *apiprocess.Start {
	options := attrs.NewBag()
	options.Set(apiprocess.ProcessParentKey, parent)
	options.Set(apiprocess.ProcessMonitorKey, true)
	options.Set(apiprocess.ProcessLinkKey, true)
	return &apiprocess.Start{
		HostID:  "host",
		Source:  registry.ParseID("test:source"),
		Options: options,
	}
}

func relationshipManager(t *testing.T, topo topology.Topology) (*Manager, *relationshipHost, context.Context) {
	t.Helper()
	node := newMockNode()
	host := &relationshipHost{pid: pid.PID{Node: "remote", Host: "host", UniqID: "child"}}
	require.NoError(t, node.RegisterHost("host", host))
	ctx := topology.WithTopology(ctxapi.NewRootContext(), topo)
	return NewManager(node, zap.NewNop()), host, ctx
}

func TestY10MonitorFailureCompensatesChild(t *testing.T) {
	monitorErr := errors.New("monitor failed")
	topo := &relationshipTopology{monitorErr: monitorErr, monitored: make(map[string]struct{}), linked: make(map[string]struct{})}
	manager, host, ctx := relationshipManager(t, topo)

	got, err := manager.Start(ctx, relationshipStart(pid.PID{Node: "local", Host: "parent", UniqID: "parent"}))

	require.ErrorIs(t, err, monitorErr)
	require.Equal(t, "child", got.UniqID)
	require.Equal(t, 1, host.terminations())
	require.Empty(t, topo.monitored)
	require.Empty(t, topo.linked)
}

func TestY11LinkFailureCompensatesChild(t *testing.T) {
	linkErr := errors.New("link failed")
	topo := &relationshipTopology{linkErr: linkErr, monitored: make(map[string]struct{}), linked: make(map[string]struct{})}
	manager, host, ctx := relationshipManager(t, topo)

	got, err := manager.Start(ctx, relationshipStart(pid.PID{Node: "local", Host: "parent", UniqID: "parent"}))

	require.ErrorIs(t, err, linkErr)
	require.Equal(t, "child", got.UniqID)
	require.Equal(t, 1, host.terminations())
	require.Empty(t, topo.monitored)
	require.Empty(t, topo.linked)
}
