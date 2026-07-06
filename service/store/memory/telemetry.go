// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"errors"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/store"
)

const (
	memOpsTotal   = "wippy_kv_ops_total"
	memOpDuration = "wippy_kv_op_duration_seconds"
)

func storeErrResult(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, store.ErrKeyNotFound) || errors.Is(err, store.ErrKeyExists) || errors.Is(err, store.ErrVersionMismatch) {
		return "not_found"
	}
	return "error"
}

// recordOp emits per-operation count and latency. Nil-safe.
func recordOp(coll metrics.Collector, namespace, op, result string, duration time.Duration) {
	if coll == nil {
		return
	}
	labels := metrics.Labels{"namespace": namespace, "op": op, "result": result}
	coll.CounterInc(memOpsTotal, labels)
	coll.HistogramObserve(memOpDuration, duration.Seconds(), labels)
}
