// SPDX-License-Identifier: MPL-2.0

package clock

import (
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

// fireFailedMetric counts clock fires that did not reach their process, by
// reason: "undelivered" for a rejected send, "panic" for a failed build.
const fireFailedMetric = "clock_fire_failed_total"

// fireReporter delivers clock fires and reports every fire that does not
// arrive. A fire to a process that has already finished is expected and
// stays quiet.
type fireReporter struct {
	log  *zap.Logger
	coll metrics.Collector
}

// send delivers pkg. The sender keeps ownership of a rejected package, so a
// rejected package is released here.
func (r *fireReporter) send(node relay.Node, pkg *relay.Package) {
	target, topic := pkg.Target, pkg.Messages[0].Topic
	err := node.Send(pkg)
	if err == nil {
		return
	}
	relay.ReleasePackage(pkg)
	if errors.Is(err, process.ErrProcessNotFound) || errors.Is(err, process.ErrProcessClosed) {
		return
	}
	r.log.Warn("clock fire not delivered",
		zap.String("target", target.String()), zap.String("topic", topic), zap.Error(err))
	r.count("undelivered")
}

// run executes fire and reports a panic with its stack instead of letting
// it end the timer or ticker goroutine and the runtime with it.
func (r *fireReporter) run(target pid.PID, fire func()) {
	defer func() {
		if v := recover(); v != nil {
			r.log.Error("clock fire panicked",
				zap.String("target", target.String()), zap.String("panic", fmt.Sprint(v)), zap.Stack("stack"))
			r.count("panic")
		}
	}()
	fire()
}

func (r *fireReporter) count(reason string) {
	if r.coll != nil {
		r.coll.CounterInc(fireFailedMetric, metrics.Labels{"reason": reason})
	}
}
