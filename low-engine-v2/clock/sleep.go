package clock

import (
	"time"

	"github.com/wippyai/runtime/low-engine-v2/scheduler"
)

// Command IDs for time subsystem (10-49 reserved range)
const (
	CmdSleep scheduler.CommandID = 10
)

// SleepCmd requests the scheduler to pause the process for a duration.
type SleepCmd struct {
	Duration time.Duration
}

func (s SleepCmd) CmdID() scheduler.CommandID { return CmdSleep }

// SleepHandler handles SleepCmd by spawning a goroutine that sleeps.
// Uses time.AfterFunc for efficient timer management.
type SleepHandler struct{}

func NewSleepHandler() *SleepHandler {
	return &SleepHandler{}
}

func (h *SleepHandler) Handle(cmd scheduler.Command, proc *scheduler.Processor) {
	sleep := cmd.(SleepCmd)

	// Simple: just schedule callback, no goroutine
	// Cancellation is handled by scheduler when process dies
	time.AfterFunc(sleep.Duration, func() {
		proc.Complete(nil, nil)
	})
}

// Register registers the sleep handler with a scheduler registry.
func Register(r *scheduler.Registry) {
	r.Register(CmdSleep, NewSleepHandler())
}
