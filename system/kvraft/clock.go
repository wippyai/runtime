// SPDX-License-Identifier: MPL-2.0

package kvraft

import "time"

// ourTimeNow is the time source used by the FSM for TTL evaluation. Tests
// can replace it for deterministic expiration.
var ourTimeNow = time.Now
