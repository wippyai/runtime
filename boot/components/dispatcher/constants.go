package dispatcher

import "github.com/wippyai/runtime/api/boot"

const (
	ClockName      boot.ComponentName = "dispatcher.clock"
	DispatcherDeps boot.ComponentName = "dispatcher" // local ref to avoid import cycle
)
