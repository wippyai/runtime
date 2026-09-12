// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"errors"

	"github.com/wippyai/runtime/api/pid"
)

// remoteMonitorRetention belongs to the same target lifetime as its monitor
// set. Set transitions hold the set mutex before the outbox mutex; the outbox
// never calls back into the set. Completion inherits reservations returned by
// close and must retain them until delivery is settled.
type remoteMonitorRetention struct {
	outbox    *monitorOutbox
	target    pid.PID
	allowance int64
}

func newRetainedRemoteMonitorSet(maximum int, outbox *monitorOutbox, target pid.PID, allowance int64) (*remoteMonitorSet, error) {
	if outbox == nil || target.Node == "" || target.Host == "" || target.UniqID == "" || allowance <= 0 || allowance > outbox.maximumBytes {
		return nil, errors.New("invalid remote monitor retention configuration")
	}
	set, err := newRemoteMonitorSet(maximum)
	if err != nil {
		return nil, err
	}
	set.retention = &remoteMonitorRetention{outbox: outbox, target: target, allowance: allowance}
	return set, nil
}
