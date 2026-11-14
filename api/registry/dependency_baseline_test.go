// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDependencyBaselineContext(t *testing.T) {
	_, ok := DependencyBaselineFromContext(context.Background())
	require.False(t, ok)
	_, ok = DependencyBaselineFromContext(nil)
	require.False(t, ok)
	require.Nil(t, DependencyChangesFromContext(nil))

	ctx := WithDependencyBaseline(context.Background(), nil, nil)
	state, ok := DependencyBaselineFromContext(ctx)
	require.True(t, ok, "an explicitly empty deployment is still known")
	require.Nil(t, state)

	baseline := State{{ID: NewID("source", "root"), Kind: NamespaceDependency}}
	changes := []ChangeSet{{{Kind: EntryDelete, Entry: baseline[0]}}}
	ctx = WithDependencyBaseline(ctx, baseline, changes)
	state, ok = DependencyBaselineFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, baseline, state)
	require.Equal(t, changes, DependencyChangesFromContext(ctx))
}

func TestDependencyReplayStateContext(t *testing.T) {
	baseline := State{{ID: NewID("source", "root"), Kind: NamespaceDependency}}
	changes := []ChangeSet{{{Kind: EntryDelete, Entry: baseline[0]}}}
	projection := NewReplayState()
	ctx := WithDependencyBaseline(context.Background(), baseline, changes)
	ctx = WithDependencyReplayState(ctx, baseline, projection)

	got, ok := DependencyReplayStateFromContext(ctx)
	require.True(t, ok)
	require.Same(t, projection, got)
	require.Equal(t, changes, DependencyChangesFromContext(ctx))
	gotBaseline, ok := DependencyBaselineFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, baseline, gotBaseline)
}

func TestDependencyBaselineReplacesInheritedReplayState(t *testing.T) {
	projection := NewReplayState()
	ctx := WithDependencyReplayState(context.Background(), nil, projection)
	replacement := WithDependencyBaseline(ctx, State{{ID: NewID("test", "baseline")}}, nil)
	_, present := DependencyReplayStateFromContext(replacement)
	require.False(t, present, "an explicit baseline replaces the previous operation's replay projection")
}
