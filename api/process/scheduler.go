package process

import "errors"

// SchedulerKind identifies the scheduler implementation.
type SchedulerKind string

const (
	// KindGlobal uses a global queue only. Good for IO-bound workloads.
	KindGlobal SchedulerKind = "global"

	// KindStealing uses work-stealing with local deques. Good for CPU-bound workloads.
	KindStealing SchedulerKind = "stealing"
)

// ErrMaxProcessesExceeded is returned when max process limit is reached.
var ErrMaxProcessesExceeded = errors.New("max processes limit exceeded")
