package host

import (
	stdcontext "context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

type mockProcess struct {
	stepResult process.StepResult
}

func (m *mockProcess) Start(stdcontext.Context, relay.PID, payload.Payloads) error {
	return nil
}

func (m *mockProcess) Step() (process.StepResult, error) {
	if m.stepResult == 0 {
		return process.StepIdle, nil
	}
	return m.stepResult, nil
}

func (m *mockProcess) Send(*relay.Package) error {
	return nil
}

func (m *mockProcess) Terminate() {}

type mockInfoProcess struct {
	mockProcess
	info attrs.Bag
}

func (m *mockInfoProcess) Info() (attrs.Bag, error) {
	return m.info, nil
}

func TestProcessPool_StatsDisabledByDefault(t *testing.T) {
	ctx := stdcontext.Background()
	pool := NewProcessPool(ctx, 1, 10, zap.NewNop())
	defer pool.Close()

	enabled, _, _, err := pool.Collect(ctx)
	require.NoError(t, err)
	assert.False(t, enabled)
}

func TestProcessPool_EnableDisableStats(t *testing.T) {
	ctx := stdcontext.Background()
	pool := NewProcessPool(ctx, 1, 10, zap.NewNop())
	defer pool.Close()

	pool.EnableStats(50)
	enabled, sampleRate, _, err := pool.Collect(ctx)
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, int64(50), sampleRate)

	pool.DisableStats()
	enabled, _, _, err = pool.Collect(ctx)
	require.NoError(t, err)
	assert.False(t, enabled)
}

func TestProcessPool_CollectBasicStats(t *testing.T) {
	ctx := stdcontext.Background()
	pool := NewProcessPool(ctx, 2, 10, zap.NewNop())
	pool.Start()
	defer pool.Close()

	pool.EnableStats(1)

	pid := relay.PID{UniqID: "test-process"}
	sourceID := registry.ID{NS: "test", Name: "source"}

	proc := &mockInfoProcess{
		mockProcess: mockProcess{stepResult: process.StepIdle},
		info: attrs.Bag{
			"custom_field": "test_value",
		},
	}

	launch := &process.Launch{
		PID:     pid,
		Source:  sourceID,
		Process: proc,
	}

	err := pool.Add(ctx, launch)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	enabled, sampleRate, entries, err := pool.Collect(ctx)
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, int64(1), sampleRate)
	assert.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, pid.UniqID, entry.PID.UniqID)
	assert.Equal(t, sourceID.String(), entry.SourceID)
	assert.Greater(t, entry.StepCount, int64(0))
	assert.False(t, entry.StartedAt.IsZero())
	assert.False(t, entry.LastActivityAt.IsZero())
}

func TestProcessPool_InfoSamplingEveryN(t *testing.T) {
	ctx := stdcontext.Background()
	pool := NewProcessPool(ctx, 2, 10, zap.NewNop())
	pool.Start()
	defer pool.Close()

	pool.EnableStats(3)

	pid := relay.PID{UniqID: "test-sampler"}
	sourceID := registry.ID{NS: "test", Name: "sampler"}

	proc := &mockInfoProcess{
		mockProcess: mockProcess{stepResult: process.StepContinue},
		info: attrs.Bag{
			"iteration": "initial",
		},
	}

	launch := &process.Launch{
		PID:     pid,
		Source:  sourceID,
		Process: proc,
	}

	err := pool.Add(ctx, launch)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		err = pool.Schedule(pid)
		require.NoError(t, err)
		time.Sleep(5 * time.Millisecond)
	}

	pool.Remove(pid)
}

func TestProcessPool_StatsWithNonInfoProvider(t *testing.T) {
	ctx := stdcontext.Background()
	pool := NewProcessPool(ctx, 2, 10, zap.NewNop())
	pool.Start()
	defer pool.Close()

	pool.EnableStats(1)

	pid := relay.PID{UniqID: "non-info-process"}
	sourceID := registry.ID{NS: "test", Name: "non-info"}

	proc := &mockProcess{stepResult: process.StepIdle}

	launch := &process.Launch{
		PID:     pid,
		Source:  sourceID,
		Process: proc,
	}

	err := pool.Add(ctx, launch)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	enabled, _, entries, err := pool.Collect(ctx)
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Len(t, entries, 1)

	entry := entries[0]
	assert.Nil(t, entry.Info)
}
