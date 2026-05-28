// SPDX-License-Identifier: MPL-2.0

package pg

import "github.com/wippyai/runtime/api/pid"

// Event-bus contract for process-group membership changes: the system name,
// the join/left kinds, and the payload carried on each event.

// Event bus constants for pg membership events.
const (
	// EventSystem is the event bus system name for pg events.
	EventSystem = "pg"

	// MemberJoined is the event kind emitted when processes join a group.
	MemberJoined = "member.joined"

	// MemberLeft is the event kind emitted when processes leave a group.
	MemberLeft = "member.left"
)

// MembershipEvent is the data payload for pg membership change events.
type MembershipEvent struct {
	Group string    // The group that changed
	PIDs  []pid.PID // The PIDs that joined or left
}
