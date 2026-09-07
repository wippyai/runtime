// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cconfig "github.com/wippyai/runtime/api/service/cdc"
)

func TestSourceAdapterInfoReportsConservativeCapabilities(t *testing.T) {
	source := NewSource(SourceOptions{
		Name:      "app:events",
		Slot:      "events_slot",
		Snapshot:  true,
		Streaming: true,
		Temporary: true,
	})
	terminalErr := errors.New("replication connection lost")
	source.mu.Lock()
	source.state = sourceFailed
	source.sourceErr = terminalErr
	source.mu.Unlock()

	adapter := &sourceAdapter{source: source}
	info := adapter.Info()

	assert.Equal(t, cconfig.SourceStateFaulted, info.State)
	assert.True(t, info.Faulted)
	assert.Equal(t, terminalErr.Error(), info.Error)
	assert.True(t, info.Snapshot, "entry snapshot field preserves the configured subscriber default")
	assert.True(t, info.Streaming, "legacy streaming field preserves configured protocol mode")
	assert.True(t, info.Capabilities.Snapshot, "per-consumer snapshots use the atomic handoff")
	assert.False(t, info.Capabilities.Replayable, "After cursors are unsupported")
	assert.False(t, info.Capabilities.CaptureResume, "temporary slots are not durable")
	assert.False(t, info.Capabilities.BeforeImages)
}

func TestSourceAdapterSnapshotRequiresRunningGeneration(t *testing.T) {
	source := NewSource(SourceOptions{Slot: "events_slot"})
	adapter := &sourceAdapter{source: source}
	_, err := adapter.Subscribe(context.Background(), cconfig.StreamOptions{Snapshot: true})
	assert.ErrorIs(t, err, cconfig.ErrSourceNotReady)
}

func TestSourceStopAfterDisposeCleanupIsIdempotent(t *testing.T) {
	source := NewSource(SourceOptions{Slot: "events_slot"})
	source.dropDone.Store(true)
	source.MarkForSlotDrop()

	// A completed cleanup is a tombstone: retries must not reopen a connection
	// or attempt to drop the same slot again.
	require.NoError(t, source.Stop(nil))
	require.NoError(t, source.Stop(nil))
}

func TestPostgresExclusiveKeyIsClusterWide(t *testing.T) {
	first := &cconfig.Config{Host: "db.internal", Port: 5432, Database: "one", SlotName: "events"}
	second := &cconfig.Config{Host: "db.internal", Port: 5432, Database: "two", SlotName: "events"}

	assert.Equal(t, postgresExclusiveKey(first), postgresExclusiveKey(second))
	assert.NotContains(t, postgresExclusiveKey(first), "password")
}
