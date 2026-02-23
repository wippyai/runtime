// SPDX-License-Identifier: MPL-2.0

package system

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
)

type mockInspector struct {
	processes map[string][]process.Stats
	hosts     []process.HostStats
}

func (m *mockInspector) ListHosts() []process.HostStats {
	return m.hosts
}

func (m *mockInspector) HostProcesses(hostID registry.ID) []process.Stats {
	if m.processes == nil {
		return nil
	}
	return m.processes[hostID.String()]
}

func TestHostsTableExists(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	checkTable(t, l, "system", "hosts")
}

func TestHostsListNoInspector(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	err := l.DoString(`
		local hosts, err = system.hosts.list()
		assert(hosts == nil, "expected nil hosts")
		assert(err ~= nil, "expected error")
	`)
	require.NoError(t, err)
}

func TestHostsProcessesNoInspector(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	err := l.DoString(`
		local procs, err = system.hosts.processes("test:host")
		assert(procs == nil, "expected nil procs")
		assert(err ~= nil, "expected error")
	`)
	require.NoError(t, err)
}

func TestHostsListWithInspector(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	inspector := &mockInspector{
		hosts: []process.HostStats{
			{
				ID:         registry.NewID("test", "host-a"),
				Workers:    4,
				Processes:  10,
				Executed:   1000,
				Stolen:     50,
				QueueDepth: 3,
			},
			{
				ID:         registry.NewID("test", "host-b"),
				Workers:    2,
				Processes:  5,
				Executed:   500,
				Stolen:     20,
				QueueDepth: 0,
			},
		},
	}
	ctx = process.WithInspector(ctx, inspector)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	err := l.DoString(`
		local hosts, err = system.hosts.list()
		assert(err == nil, "expected nil error, got: " .. tostring(err))
		assert(type(hosts) == "table", "expected table")
		assert(#hosts == 2, "expected 2 hosts, got " .. #hosts)

		assert(hosts[1].id == "test:host-a", "expected test:host-a, got " .. hosts[1].id)
		assert(hosts[1].workers == 4, "expected 4 workers")
		assert(hosts[1].processes == 10, "expected 10 processes")
		assert(hosts[1].executed == 1000, "expected 1000 executed")
		assert(hosts[1].stolen == 50, "expected 50 stolen")
		assert(hosts[1].queue_depth == 3, "expected 3 queue_depth")

		assert(hosts[2].id == "test:host-b", "expected test:host-b")
		assert(hosts[2].workers == 2, "expected 2 workers")
	`)
	require.NoError(t, err)
}

func TestHostsProcessesWithInspector(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	hostID := registry.NewID("test", "host-a")
	inspector := &mockInspector{
		processes: map[string][]process.Stats{
			hostID.String(): {
				{
					PID:       pidapi.PID{Node: "n1", UniqID: "100"},
					Host:      hostID,
					Source:    registry.NewID("apps", "myapp"),
					State:     "running",
					Steps:     42,
					StartedAt: 1700000000000000000,
				},
				{
					PID:       pidapi.PID{Node: "n1", UniqID: "101"},
					Parent:    pidapi.PID{Node: "n1", UniqID: "100"},
					Host:      hostID,
					Source:    registry.NewID("services", "worker"),
					State:     "idle",
					Steps:     7,
					StartedAt: 1700000001000000000,
					Stats:     attrs.Bag{"requests": 150, "errors": 3},
				},
			},
		},
	}
	ctx = process.WithInspector(ctx, inspector)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	err := l.DoString(`
		local procs, err = system.hosts.processes("test:host-a")
		assert(err == nil, "expected nil error, got: " .. tostring(err))
		assert(type(procs) == "table", "expected table")
		assert(#procs == 2, "expected 2 processes, got " .. #procs)

		assert(procs[1].state == "running", "expected running")
		assert(procs[1].steps == 42, "expected 42 steps")
		assert(procs[1].started_at == 1700000000000000000, "expected started_at")
		assert(procs[1].parent == nil, "expected no parent for root process")

		assert(procs[2].state == "idle", "expected idle")
		assert(procs[2].steps == 7, "expected 7 steps")
		assert(procs[2].started_at == 1700000001000000000, "expected started_at")
		assert(type(procs[2].parent) == "string", "expected parent PID string")
		assert(type(procs[2].stats) == "table", "expected stats table")
		assert(procs[2].stats.requests == 150, "expected 150 requests")
		assert(procs[2].stats.errors == 3, "expected 3 errors")
	`)
	require.NoError(t, err)
}

func TestHostsProcessesEmptyHost(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	inspector := &mockInspector{
		processes: map[string][]process.Stats{},
	}
	ctx = process.WithInspector(ctx, inspector)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	err := l.DoString(`
		local procs, err = system.hosts.processes("unknown-host")
		assert(err == nil, "expected nil error")
		assert(type(procs) == "table", "expected table")
		assert(#procs == 0, "expected 0 processes")
	`)
	require.NoError(t, err)
}

func TestHostsListEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	inspector := &mockInspector{}
	ctx = process.WithInspector(ctx, inspector)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	err := l.DoString(`
		local hosts, err = system.hosts.list()
		assert(err == nil, "expected nil error")
		assert(type(hosts) == "table", "expected table")
		assert(#hosts == 0, "expected 0 hosts")
	`)
	require.NoError(t, err)
}

func TestHostsPermissionDenied(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), true)
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	t.Run("list", func(t *testing.T) {
		err := l.DoString(`
			local hosts, err = system.hosts.list()
			assert(hosts == nil, "expected nil hosts")
			assert(err ~= nil, "expected error")
			assert(err:kind() == errors.INVALID, "expected INVALID kind")
		`)
		assert.NoError(t, err)
	})

	t.Run("processes", func(t *testing.T) {
		err := l.DoString(`
			local procs, err = system.hosts.processes("test:host")
			assert(procs == nil, "expected nil procs")
			assert(err ~= nil, "expected error")
			assert(err:kind() == errors.INVALID, "expected INVALID kind")
		`)
		assert.NoError(t, err)
	})
}
