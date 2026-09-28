// SPDX-License-Identifier: MPL-2.0

package composite

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/runtime/system/registry/history/historytest"
	"github.com/wippyai/runtime/system/registry/history/memory"
)

func TestConformance(t *testing.T) {
	historytest.Run(t, func(*testing.T) historytest.History { return New(memory.New()) })
}

func TestSwitchConformance(t *testing.T) {
	historytest.Run(t, func(t *testing.T) historytest.History {
		h := New(memory.New())
		require.NoError(t, h.Switch(func(Driver, registry.State) (Driver, error) { return memory.New(), nil }))
		return h
	})
}

func TestTransactionKeepsDriver(t *testing.T) {
	first, second := memory.New(), memory.New()
	h := New(first)
	end := h.BeginTransaction()

	switched := make(chan error, 1)
	go func() {
		switched <- h.Switch(func(current Driver, _ registry.State) (Driver, error) {
			require.Same(t, first, current)
			return second, nil
		})
	}()

	v1 := version.FromParent(version.New(0), 1)
	require.NoError(t, h.Save(v1, historytest.Changes("one", "first"), true))
	select {
	case err := <-switched:
		t.Fatalf("switch finished during a transaction: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	v2 := version.FromParent(v1, 2)
	require.NoError(t, h.Save(v2, historytest.Changes("two", "second"), true))
	end()

	require.NoError(t, <-switched)
	require.Same(t, second, h.Active())
	_, err := first.GetVersion(2)
	require.NoError(t, err)
	_, err = second.GetVersion(1)
	require.Error(t, err)
}

func TestFailedSwitchKeepsDriver(t *testing.T) {
	first := memory.New()
	h := New(first)
	failed := errors.New("transfer failed")
	require.ErrorIs(t, h.Switch(func(Driver, registry.State) (Driver, error) { return nil, failed }), failed)
	require.Same(t, first, h.Active())
	end := h.BeginTransaction()
	end()
}

type baselineDriver struct {
	*memory.Storage
	state registry.State
}

func (d *baselineDriver) Baseline() (registry.State, error) {
	if d.state == nil {
		return nil, registry.ErrBaselineNotFound
	}
	return d.state, nil
}

func (d *baselineDriver) SaveBaseline(state registry.State) error {
	d.state = state
	return nil
}

func TestBaseline(t *testing.T) {
	h := New(memory.New())
	_, err := h.Baseline()
	require.ErrorIs(t, err, registry.ErrBaselineNotFound)
	baseline := registry.State{historytest.Entry("base", "value")}
	require.NoError(t, h.SaveBaseline(baseline))

	remote := &baselineDriver{Storage: memory.New()}
	require.NoError(t, h.Switch(func(_ Driver, current registry.State) (Driver, error) {
		require.Equal(t, baseline, current)
		return remote, remote.SaveBaseline(current)
	}))
	stored, err := h.Baseline()
	require.NoError(t, err)
	require.Equal(t, baseline, stored)
	require.NoError(t, h.SaveBaseline(nil))
	require.Nil(t, remote.state)
}
