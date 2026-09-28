// SPDX-License-Identifier: MPL-2.0

package remote

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	historyv1 "github.com/wippyai/runtime/api/registry/history/v1"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/runtime/system/registry/history/historytest"
	"github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/history/remote/remotetest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func open(t *testing.T, registryID string) (*History, *remotetest.Server) {
	t.Helper()
	server, connection, stop := remotetest.Start()
	t.Cleanup(stop)
	history, err := New(connection, Config{Key: testKey(registryID), Timeout: 5 * time.Second})
	require.NoError(t, err)
	return history, server
}

func testKey(registryID string) *historyv1.RegistryKey {
	return &historyv1.RegistryKey{TenantId: "tenant", EnvironmentId: "stage", RegistryId: registryID}
}

func TestConformance(t *testing.T) {
	historytest.Run(t, func(t *testing.T) historytest.History {
		history, _ := open(t, "app")
		return history
	})
}

func failOnce(method string, after bool) func(string, bool) error {
	failed := false
	return func(called string, calledAfter bool) error {
		if failed || called != method || calledAfter != after {
			return nil
		}
		failed = true
		return status.Error(codes.Unavailable, "connection lost")
	}
}

func TestSaveRetriesAfterUnknownResult(t *testing.T) {
	history, server := open(t, "app")
	server.Fail = failOnce("Save", true)
	v1 := version.FromParent(version.New(0), 1)
	require.NoError(t, history.SaveWithDependencyResolution(v1, historytest.Changes("one", "first"), historytest.Resolution("first", ""), true))
	head, err := history.Head()
	require.NoError(t, err)
	require.Equal(t, uint(1), head.ID())
	versions, err := history.Versions()
	require.NoError(t, err)
	require.Len(t, versions, 2)
}

func TestSaveRetryDoesNotHideConflict(t *testing.T) {
	history, server := open(t, "app")
	v1 := version.FromParent(version.New(0), 1)
	require.NoError(t, history.Save(v1, historytest.Changes("one", "first"), true))
	server.Fail = failOnce("Save", false)
	require.Error(t, history.Save(v1, historytest.Changes("one", "other"), false))
}

func TestCompareAndSetHeadRetriesAfterUnknownResult(t *testing.T) {
	history, server := open(t, "app")
	v1 := version.FromParent(version.New(0), 1)
	v2 := version.FromParent(v1, 2)
	require.NoError(t, history.Save(v1, historytest.Changes("one", "first"), true))
	require.NoError(t, history.Save(v2, historytest.Changes("two", "second"), true))
	server.Fail = failOnce("CompareAndSetHead", true)
	require.NoError(t, history.CompareAndSetHead(v2, v1))
	head, err := history.Head()
	require.NoError(t, err)
	require.Equal(t, uint(1), head.ID())
}

func TestReadDoesNotRetryDeadline(t *testing.T) {
	history, server := open(t, "app")
	calls := 0
	server.Fail = func(method string, after bool) error {
		if method == "Head" && !after {
			calls++
			return status.Error(codes.DeadlineExceeded, "slow")
		}
		return nil
	}
	_, err := history.Head()
	require.Error(t, err)
	require.Equal(t, 1, calls)
}

func TestBaseline(t *testing.T) {
	history, server := open(t, "app")
	_, err := history.Baseline()
	require.ErrorIs(t, err, registry.ErrBaselineNotFound)
	baseline := registry.State{historytest.Entry("one", "first"), historytest.Entry("two", "second")}
	require.NoError(t, history.SaveBaseline(baseline))
	calls := 0
	server.Fail = func(method string, after bool) error {
		if method == "SetBaseline" && !after {
			calls++
		}
		return nil
	}
	require.NoError(t, history.SaveBaseline(baseline))
	require.Zero(t, calls)
	stored, err := history.Baseline()
	require.NoError(t, err)
	require.Len(t, stored, 2)
	require.Equal(t, baseline[1].ID, stored[1].ID)
}

type sourceFixture struct {
	history  *memory.Storage
	baseline registry.State
}

func source(t *testing.T) sourceFixture {
	t.Helper()
	h := memory.New()
	root := version.New(0)
	v1 := version.FromParent(root, 1)
	v2 := version.FromParent(v1, 2)
	v3 := version.FromParent(v1, 3)
	require.NoError(t, h.SaveWithDependencyResolution(v1, historytest.Changes("one", "first"), historytest.Resolution("first", "base"), true))
	require.NoError(t, h.Save(v2, historytest.Changes("two", "second"), true))
	require.NoError(t, h.SetHead(v1))
	require.NoError(t, h.SaveWithDependencyResolution(v3, historytest.Changes("three", "third"), historytest.Resolution("third", "base"), true))
	return sourceFixture{history: h, baseline: registry.State{historytest.Entry("base", "value")}}
}

func requireSameHistory(t *testing.T, expected *memory.Storage, actual *History) {
	t.Helper()
	expectedVersions, err := expected.Versions()
	require.NoError(t, err)
	actualVersions, err := actual.Versions()
	require.NoError(t, err)
	require.Len(t, actualVersions, len(expectedVersions))
	for i, v := range expectedVersions {
		require.Equal(t, v.ID(), actualVersions[i].ID())
		if v.ID() == registry.RootVersion {
			continue
		}
		require.Equal(t, v.Previous().ID(), actualVersions[i].Previous().ID())
		expectedChanges, err := expected.Get(v)
		require.NoError(t, err)
		actualChanges, err := actual.Get(v)
		require.NoError(t, err)
		require.Len(t, actualChanges, len(expectedChanges))
		expectedResolution, expectedErr := expected.GetDependencyResolution(v)
		actualResolution, actualErr := actual.GetDependencyResolution(v)
		require.Equal(t, expectedErr, actualErr)
		if expectedErr == nil {
			require.Equal(t, expectedResolution.Digest, actualResolution.Digest)
		}
	}
	expectedHead, err := expected.Head()
	require.NoError(t, err)
	actualHead, err := actual.Head()
	require.NoError(t, err)
	require.Equal(t, expectedHead.ID(), actualHead.ID())
}

func TestTransfer(t *testing.T) {
	fixture := source(t)
	history, _ := open(t, "app")
	require.NoError(t, history.Transfer(context.Background(), "transfer-1", fixture.history, fixture.baseline))
	requireSameHistory(t, fixture.history, history)
	baseline, err := history.Baseline()
	require.NoError(t, err)
	require.Len(t, baseline, 1)
	require.Equal(t, fixture.baseline[0].ID, baseline[0].ID)

	require.NoError(t, history.Transfer(context.Background(), "transfer-1", fixture.history, fixture.baseline))
	v4 := version.FromParent(version.FromParent(version.FromParent(version.New(0), 1), 3), 4)
	require.NoError(t, history.Save(v4, historytest.Changes("four", "fourth"), true))
	require.NoError(t, history.Transfer(context.Background(), "transfer-1", fixture.history, fixture.baseline))
	head, err := history.Head()
	require.NoError(t, err)
	require.Equal(t, uint(4), head.ID())

	changed := source(t)
	require.NoError(t, changed.history.Save(version.FromParent(version.FromParent(version.FromParent(version.New(0), 1), 3), 5), historytest.Changes("five", "fifth"), true))
	require.ErrorContains(t, history.Transfer(context.Background(), "transfer-1", changed.history, fixture.baseline), "differs")
}

func TestInterruptedTransferResumes(t *testing.T) {
	fixture := source(t)
	history, server := open(t, "app")
	server.Fail = func(method string, after bool) error {
		if method == "CompleteTransfer" && !after {
			return status.Error(codes.Unavailable, "connection lost")
		}
		return nil
	}
	require.Error(t, history.Transfer(context.Background(), "transfer-1", fixture.history, fixture.baseline))
	_, err := history.Head()
	require.ErrorContains(t, err, "transfer is not complete")
	require.Error(t, history.Transfer(context.Background(), "transfer-2", fixture.history, fixture.baseline))

	server.Fail = nil
	require.NoError(t, history.Transfer(context.Background(), "transfer-1", fixture.history, fixture.baseline))
	requireSameHistory(t, fixture.history, history)

	versions, err := fixture.history.Versions()
	require.NoError(t, err)
	require.Len(t, versions, 4)
}

func TestTransferRejectsUnrelatedHistory(t *testing.T) {
	fixture := source(t)
	history, _ := open(t, "app")
	v1 := version.FromParent(version.New(0), 1)
	require.NoError(t, history.Save(v1, historytest.Changes("other", "value"), true))
	require.ErrorContains(t, history.Transfer(context.Background(), "transfer-1", fixture.history, fixture.baseline), "not empty")
	changes, err := history.Get(v1)
	require.NoError(t, err)
	require.Equal(t, "other", changes[0].Entry.ID.Name)

	completed, _ := open(t, "done")
	require.NoError(t, completed.Transfer(context.Background(), "transfer-1", fixture.history, fixture.baseline))
	require.ErrorContains(t, completed.Transfer(context.Background(), "transfer-2", fixture.history, fixture.baseline), "not empty")
}

func TestTransferEmptySource(t *testing.T) {
	history, _ := open(t, "app")
	require.NoError(t, history.Transfer(context.Background(), "transfer-1", memory.New(), nil))
	head, err := history.Head()
	require.NoError(t, err)
	require.Equal(t, registry.RootVersion, head.ID())
	baseline, err := history.Baseline()
	require.NoError(t, err)
	require.Empty(t, baseline)
}

func TestVersionCacheFollowsLineage(t *testing.T) {
	history, _ := open(t, "app")
	first, err := history.fromLineage(2, &historyv1.Lineage{Ids: []uint64{1, 2}})
	require.NoError(t, err)
	again, err := history.fromLineage(2, &historyv1.Lineage{Ids: []uint64{1, 2}})
	require.NoError(t, err)
	require.Same(t, first, again)

	changed, err := history.fromLineage(2, &historyv1.Lineage{Ids: []uint64{2}})
	require.NoError(t, err)
	require.NotSame(t, first, changed)
	require.Equal(t, registry.RootVersion, changed.Previous().ID())

	_, err = history.fromLineage(3, &historyv1.Lineage{Ids: []uint64{2}})
	require.ErrorContains(t, err, "invalid lineage")
	root, err := history.fromLineage(registry.RootVersion, &historyv1.Lineage{})
	require.NoError(t, err)
	require.Equal(t, registry.RootVersion, root.ID())
}
