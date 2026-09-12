// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"errors"
	"fmt"

	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

const strongResultPrefix = registryPrefix + "result:"
const strongResultUsageKey = registryPrefix + "result_usage"

var errStrongResultCapacity = globalapi.ErrStrongResultCapacity
var errStrongResultTooLarge = globalapi.ErrStrongResultTooLarge

type strongResultLimits struct{ Entries, Bytes uint64 }
type strongResultUsage struct {
	Entries uint64 `codec:"n"`
	Bytes   uint64 `codec:"b"`
}

// Capacity includes the maximum serialized value and key bytes, not only the
// initial placeholder. Completion must fit its already-admitted reservation.
// Admission and completion share this slot; leader reclamation releases quota.
type strongResultReservation struct {
	Capacity uint64 `codec:"c"`
	Data     []byte `codec:"d"`
}

func strongResultKey(attempt string) string { return strongResultPrefix + attempt }

func readStrongResultUsage(read func(string) (kvapi.Entry, error)) (strongResultUsage, kvapi.TxnOp, error) {
	entry, err := read(strongResultUsageKey)
	if errors.Is(err, kvapi.ErrKeyNotFound) {
		return strongResultUsage{}, kvapi.TxnOp{Kind: kvapi.TxnCheck, Cond: kvapi.CondAbsent, Key: strongResultUsageKey}, nil
	}
	if err != nil {
		return strongResultUsage{}, kvapi.TxnOp{}, err
	}
	var usage strongResultUsage
	if err := decodeInto(entry.Value, &usage); err != nil {
		return usage, kvapi.TxnOp{}, err
	}
	if entry.Version == 0 || ((usage.Entries == 0 && usage.Bytes != 0) || usage.Entries > usage.Bytes) {
		return usage, kvapi.TxnOp{}, fmt.Errorf("invalid Strong result usage record")
	}
	return usage, kvapi.TxnOp{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: strongResultUsageKey, Expect: entry.Version}, nil
}

// reserveStrongResultOps must be committed in the SAME transaction as pending
// creation. Returning these ops does not reserve capacity. On CAS rejection the
// caller must reread usage; it must not replay stale operations indefinitely.
func reserveStrongResultOps(read func(string) (kvapi.Entry, error), limits strongResultLimits, attempt string, capacity uint64, data []byte) ([]kvapi.TxnOp, error) {
	if read == nil || limits.Entries == 0 || limits.Bytes == 0 || attempt == "" || capacity == 0 {
		return nil, fmt.Errorf("invalid Strong result reservation")
	}
	// Reject oversized input before building its key or encoding its payload.
	if capacity > limits.Bytes || uint64(len(strongResultPrefix)) > capacity || uint64(len(attempt)) > capacity-uint64(len(strongResultPrefix)) {
		return nil, errStrongResultTooLarge
	}
	key := strongResultKey(attempt)
	if uint64(len(data)) > capacity-uint64(len(key)) {
		return nil, errStrongResultTooLarge
	}
	body, err := encode(strongResultReservation{Capacity: capacity, Data: data})
	if err != nil {
		return nil, err
	}
	// Subtraction avoids overflow when evaluating a hostile/invalid byte budget.
	if uint64(len(key)) > capacity || uint64(len(body)) > capacity-uint64(len(key)) {
		return nil, errStrongResultTooLarge
	}
	usage, check, err := readStrongResultUsage(read)
	if err != nil {
		return nil, err
	}
	if usage.Entries >= limits.Entries || usage.Bytes > limits.Bytes || capacity > limits.Bytes-usage.Bytes {
		return nil, errStrongResultCapacity
	}
	usage.Entries++
	usage.Bytes += capacity
	encoded, err := encode(usage)
	if err != nil {
		return nil, err
	}
	return []kvapi.TxnOp{
		check,
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: key, Value: body},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: strongResultUsageKey, Value: encoded},
	}, nil
}

// releaseStrongResultOps releases only an exact observed reservation version.
// The caller must first prove a terminal result is eligible for ACK/retention
// reclamation. This function provides accounting/fencing, not that authority.
func releaseStrongResultOps(read func(string) (kvapi.Entry, error), entry kvapi.Entry) ([]kvapi.TxnOp, error) {
	if read == nil || entry.Version == 0 || len(entry.Key) <= len(strongResultPrefix) || entry.Key[:len(strongResultPrefix)] != strongResultPrefix {
		return nil, fmt.Errorf("invalid Strong result release")
	}
	var reservation strongResultReservation
	if err := decodeInto(entry.Value, &reservation); err != nil {
		return nil, err
	}
	if uint64(len(entry.Key)) > reservation.Capacity || uint64(len(entry.Value)) > reservation.Capacity-uint64(len(entry.Key)) {
		return nil, fmt.Errorf("invalid Strong result reservation capacity")
	}
	usage, check, err := readStrongResultUsage(read)
	if err != nil {
		return nil, err
	}
	if usage.Entries == 0 || usage.Bytes < reservation.Capacity {
		return nil, fmt.Errorf("Strong result usage underflow")
	}
	usage.Entries--
	usage.Bytes -= reservation.Capacity
	if usage.Entries == 0 && usage.Bytes != 0 {
		return nil, fmt.Errorf("inconsistent Strong result usage")
	}
	body, err := encode(usage)
	if err != nil {
		return nil, err
	}
	return []kvapi.TxnOp{
		check,
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondVersion, Key: entry.Key, Expect: entry.Version},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: strongResultUsageKey, Value: body},
	}, nil
}
