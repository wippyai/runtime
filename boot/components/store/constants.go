// SPDX-License-Identifier: MPL-2.0

package store

import "github.com/wippyai/runtime/api/boot"

const (
	DispatcherName  boot.Name = "store.dispatcher"
	MemStoreName    boot.Name = "store.memory"
	SQLStoreName    boot.Name = "store.sql"
	KVRaftStoreName boot.Name = "store.kv.raft"
	KVCRDTStoreName boot.Name = "store.kv.crdt"

	DispatcherDeps boot.Name = "dispatcher"
)
