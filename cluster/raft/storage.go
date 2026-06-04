// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	hraft "github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"
	wal "github.com/hashicorp/raft-wal"
)

// openStores selects the raft storage backend. An empty dataDir keeps the
// diskless in-memory stores (the default control-plane behavior). A non-empty
// dataDir opts into fs durability: a log store, a bbolt stable store for
// vote/term metadata, and a file snapshot store, all rooted at dataDir. The
// returned closer releases the durable handles on Stop; it is a no-op for the
// in-memory backend.
func openStores(dataDir string, retain int, logOut io.Writer) (
	hraft.LogStore, hraft.StableStore, hraft.SnapshotStore, func() error, error) {
	if dataDir == "" {
		return hraft.NewInmemStore(), hraft.NewInmemStore(), hraft.NewInmemSnapshotStore(),
			func() error { return nil }, nil
	}

	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("raft storage: create data dir: %w", err)
	}

	logStore, logClose, err := openLogStore(dataDir)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	stableStore, err := raftboltdb.NewBoltStore(filepath.Join(dataDir, "stable.bolt"))
	if err != nil {
		_ = logClose()
		return nil, nil, nil, nil, fmt.Errorf("raft storage: open stable store: %w", err)
	}

	if retain < 1 {
		retain = 1
	}
	snapStore, err := hraft.NewFileSnapshotStore(dataDir, retain, logOut)
	if err != nil {
		_ = logClose()
		_ = stableStore.Close()
		return nil, nil, nil, nil, fmt.Errorf("raft storage: open snapshot store: %w", err)
	}

	closer := func() error {
		errLog := logClose()
		errBolt := stableStore.Close()
		if errLog != nil {
			return errLog
		}
		return errBolt
	}
	return logStore, stableStore, snapStore, closer, nil
}

// openLogStore opens the raft LogStore. raft-wal is the default (higher write
// throughput), but it fsyncs the WAL directory during init, which returns
// "Access is denied" on Windows (directory fsync is not permitted there). On
// Windows a bbolt LogStore — the same cross-platform engine as the stable store
// — is used instead.
func openLogStore(dataDir string) (hraft.LogStore, func() error, error) {
	if runtime.GOOS == "windows" {
		return openBoltLogStore(dataDir)
	}
	return openWALLogStore(dataDir)
}

func openWALLogStore(dataDir string) (hraft.LogStore, func() error, error) {
	walDir := filepath.Join(dataDir, "wal")
	if err := os.MkdirAll(walDir, 0o750); err != nil {
		return nil, nil, fmt.Errorf("raft storage: create wal dir: %w", err)
	}
	w, err := wal.Open(walDir)
	if err != nil {
		return nil, nil, fmt.Errorf("raft storage: open wal: %w", err)
	}
	return w, w.Close, nil
}

func openBoltLogStore(dataDir string) (hraft.LogStore, func() error, error) {
	store, err := raftboltdb.NewBoltStore(filepath.Join(dataDir, "logs.bolt"))
	if err != nil {
		return nil, nil, fmt.Errorf("raft storage: open bolt log store: %w", err)
	}
	return store, store.Close, nil
}
