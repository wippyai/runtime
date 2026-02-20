package process

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
)

type stubInspector struct {
	hosts     []HostStats
	processes []Stats
}

func (s *stubInspector) ListHosts() []HostStats {
	return s.hosts
}

func (s *stubInspector) HostProcesses(_ registry.ID) []Stats {
	return s.processes
}

func TestGetInspector_NilContext(t *testing.T) {
	ctx := context.Background()
	result := GetInspector(ctx)
	assert.Nil(t, result)
}

func TestWithInspector_NoAppContext(t *testing.T) {
	ctx := context.Background()
	inspector := &stubInspector{}

	result := WithInspector(ctx, inspector)
	assert.Equal(t, ctx, result)
	assert.Nil(t, GetInspector(result))
}

func TestWithInspector_WithAppContext(t *testing.T) {
	ac := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), ac)

	inspector := &stubInspector{
		hosts: []HostStats{
			{ID: registry.NewID("ns", "host1"), Workers: 4, Processes: 10},
		},
	}

	WithInspector(ctx, inspector)

	got := GetInspector(ctx)
	require.NotNil(t, got)
	assert.Equal(t, inspector, got)
}

func TestWithInspector_SecondCallIgnored(t *testing.T) {
	ac := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), ac)

	first := &stubInspector{hosts: []HostStats{{Workers: 1}}}
	second := &stubInspector{hosts: []HostStats{{Workers: 2}}}

	WithInspector(ctx, first)
	WithInspector(ctx, second)

	got := GetInspector(ctx)
	require.NotNil(t, got)
	assert.Equal(t, first, got)
}

func TestInspector_ListHosts(t *testing.T) {
	ac := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), ac)

	hosts := []HostStats{
		{ID: registry.NewID("ns", "h1"), Workers: 2, Processes: 5, Executed: 100, Stolen: 3, QueueDepth: 7},
		{ID: registry.NewID("ns", "h2"), Workers: 4, Processes: 12, Executed: 200, Stolen: 1, QueueDepth: 0},
	}
	WithInspector(ctx, &stubInspector{hosts: hosts})

	got := GetInspector(ctx)
	require.NotNil(t, got)
	assert.Equal(t, hosts, got.ListHosts())
}

func TestInspector_HostProcesses(t *testing.T) {
	ac := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), ac)

	processes := []Stats{
		{State: "running", Steps: 42},
		{State: "blocked", Steps: 7},
	}
	WithInspector(ctx, &stubInspector{processes: processes})

	got := GetInspector(ctx)
	require.NotNil(t, got)
	hostID := registry.NewID("ns", "h1")
	assert.Equal(t, processes, got.HostProcesses(hostID))
}
