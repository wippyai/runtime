// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"time"

	"github.com/wippyai/runtime/api/metrics"
)

const (
	sqlOpsTotal   = "wippy_sql_ops_total"
	sqlOpDuration = "wippy_sql_op_duration_seconds"
)

// recordSQLOp emits per-operation count and latency. Nil-safe.
func recordSQLOp(coll metrics.Collector, op string, err error, duration time.Duration) {
	if coll == nil {
		return
	}
	result := "ok"
	if err != nil {
		result = "error"
	}
	labels := metrics.Labels{"op": op, "result": result}
	coll.CounterInc(sqlOpsTotal, labels)
	coll.HistogramObserve(sqlOpDuration, duration.Seconds(), labels)
}
