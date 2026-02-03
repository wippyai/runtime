package store

import "github.com/wippyai/runtime/api/boot"

const (
	DispatcherName boot.Name = "store.dispatcher"
	MemStoreName   boot.Name = "store.memory"
	SQLStoreName   boot.Name = "store.sql"

	DispatcherDeps boot.Name = "dispatcher"
)
