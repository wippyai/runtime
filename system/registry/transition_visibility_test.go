// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
)

// readerRunner installs the requested entries and, while the transition is in
// flight, lets a caller-supplied hook run. Entry handlers start processes in
// that window, and those processes read the registry as they boot.
type readerRunner struct {
	during func()
}

func (r *readerRunner) Transition(_ context.Context, state registry.State, changes registry.ChangeSet) (registry.State, error) {
	next := append(registry.State(nil), state...)
	for _, change := range changes {
		if change.Kind == registry.EntryCreate {
			next = append(next, change.Entry)
		}
	}
	if r.during != nil {
		r.during()
	}
	return next, nil
}

type readResult struct {
	err   error
	entry registry.Entry
}

// readDuring starts a reader from inside the transition and holds the
// transition open long enough for that read to be issued against the registry.
func readDuring(reg *Reg, id registry.ID) (func(), <-chan readResult) {
	result := make(chan readResult, 1)
	return func() {
		issued := make(chan struct{})
		go func() {
			close(issued)
			entry, err := reg.GetEntry(id)
			result <- readResult{entry: entry, err: err}
		}()
		<-issued
		time.Sleep(100 * time.Millisecond)
	}, result
}

func newVisibilityRegistry(t *testing.T, runner registry.Runner) *Reg {
	t.Helper()
	hist := historymem.New()
	require.NoError(t, hist.Save(version.New(registry.RootVersion), registry.ChangeSet{}, true))
	return NewRegistry(hist, runner, topology.NewStateBuilder(zap.NewNop(), nil), topology.NewResolver(), zap.NewNop())
}

// A process started by a transition reads the entries that transition installs.
// A read issued while the transition is in flight therefore answers with the
// transition's state, never with the state that preceded it.
func TestReadIssuedDuringApplyObservesTheAppliedState(t *testing.T) {
	config := registry.NewID("app", "config")
	runner := &readerRunner{}
	reg := newVisibilityRegistry(t, runner)

	during, result := readDuring(reg, config)
	runner.during = during

	_, err := reg.Apply(context.Background(), registry.ChangeSet{{
		Kind:  registry.EntryCreate,
		Entry: registry.Entry{ID: config, Kind: "registry.entry", Data: payload.New("db")},
	}})
	require.NoError(t, err)

	select {
	case got := <-result:
		require.NoError(t, got.err, "a reader started by the transition must find the entry it installs")
		require.Equal(t, config, got.entry.ID)
	case <-time.After(5 * time.Second):
		t.Fatal("reader never returned")
	}
}

func TestReadIssuedDuringLoadStateObservesTheLoadedState(t *testing.T) {
	config := registry.NewID("app", "config")
	runner := &readerRunner{}
	reg := newVisibilityRegistry(t, runner)

	during, result := readDuring(reg, config)
	runner.during = during

	baseline := registry.State{{ID: config, Kind: "registry.entry", Data: payload.New("db")}}
	require.NoError(t, reg.LoadState(context.Background(), baseline, version.New(registry.RootVersion)))

	select {
	case got := <-result:
		require.NoError(t, got.err, "a reader started during boot must find the loaded entry")
		require.Equal(t, config, got.entry.ID)
	case <-time.After(5 * time.Second):
		t.Fatal("reader never returned")
	}
}
