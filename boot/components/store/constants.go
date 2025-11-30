package store

import "github.com/wippyai/runtime/api/boot"

const (
	DispatcherName boot.ComponentName = "store.dispatcher"
	MemStoreName   boot.ComponentName = "store.memory"
	SQLStoreName   boot.ComponentName = "store.sql"
	TokenStoreName boot.ComponentName = "store.token"

	DispatcherDeps boot.ComponentName = "dispatcher"
)
