// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	hraft "github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"
	wal "github.com/hashicorp/raft-wal"
)

// openStores selects the raft storage backend. An empty dataDir keeps the
// diskless in-memory stores (the default control-plane behavior). A non-empty
// dataDir opts into fs durability: a raft-wal log store, a bbolt stable store
// for vote/term metadata, and a file snapshot store, all rooted at dataDir.
// The returned closer releases the durable handles on Stop; it is a no-op for
// the in-memory backend.
func openStores(dataDir string, retain int, logOut io.Writer) (
	hraft.LogStore, hraft.StableStore, hraft.SnapshotStore, func() error, error) {
	if dataDir == "" {
		return hraft.NewInmemStore(), hraft.NewInmemStore(), hraft.NewInmemSnapshotStore(),
			func() error { return nil }, nil
	}

	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("raft storage: create data dir: %w", err)
	}

	walDir := filepath.Join(dataDir, "wal")
	if err := os.MkdirAll(walDir, 0o750); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("raft storage: create wal dir: %w", err)
	}
	logStore, err := wal.Open(walDir)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("raft storage: open wal: %w", err)
	}

	stableStore, err := raftboltdb.NewBoltStore(filepath.Join(dataDir, "stable.bolt"))
	if err != nil {
		_ = logStore.Close()
		return nil, nil, nil, nil, fmt.Errorf("raft storage: open stable store: %w", err)
	}

	if retain < 1 {
		retain = 1
	}
	snapStore, err := hraft.NewFileSnapshotStore(dataDir, retain, logOut)
	if err != nil {
		_ = logStore.Close()
		_ = stableStore.Close()
		return nil, nil, nil, nil, fmt.Errorf("raft storage: open snapshot store: %w", err)
	}

	closer := func() error {
		errLog := logStore.Close()
		errBolt := stableStore.Close()
		if errLog != nil {
			return errLog
		}
		return errBolt
	}
	return logStore, stableStore, snapStore, closer, nil
}
