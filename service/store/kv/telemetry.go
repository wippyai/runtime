// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"errors"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/store/kv"
)

const (
	kvOpsTotal   = "wippy_kv_ops_total"
	kvOpDuration = "wippy_kv_op_duration_seconds"
)

// recordKVOp emits per-operation count and latency. Nil-safe.
func recordKVOp(coll metrics.Collector, namespace, op, result string, duration time.Duration) {
	if coll == nil {
		return
	}
	labels := metrics.Labels{"namespace": namespace, "op": op, "result": result}
	coll.CounterInc(kvOpsTotal, labels)
	coll.HistogramObserve(kvOpDuration, duration.Seconds(), labels)
}

// kvResult classifies an engine error into a label value.
func kvResult(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, kv.ErrKeyNotFound) {
		return "not_found"
	}
	return "error"
}
