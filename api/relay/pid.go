package relay

import "github.com/wippyai/runtime/api/pid"

// Type aliases for backwards compatibility.
type PID = pid.PID

// ParsePID delegates to pid.ParsePID.
func ParsePID(s string) (PID, error) {
	return pid.ParsePID(s)
}
