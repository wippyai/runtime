// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"time"

	api "github.com/wippyai/runtime/api/supervisor"
)

// restartWindow is owned by the controller's single lifecycle goroutine. The
// ring retains only the last MaxRestarts admissions and is allocated lazily.
type restartWindow struct {
	policy *api.RestartIntensity
	times  []time.Time
	next   int
}

func (w *restartWindow) admit(now time.Time) bool {
	if w.policy == nil {
		return true
	}
	if len(w.times) < w.policy.MaxRestarts {
		w.times = append(w.times, now)
		return true
	}
	if now.Sub(w.times[w.next]) < w.policy.Window {
		return false
	}
	w.times[w.next] = now
	w.next = (w.next + 1) % len(w.times)
	return true
}
