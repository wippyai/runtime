// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"fmt"
	"slices"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

type strongResultPhase uint8

const (
	strongResultPending strongResultPhase = iota
	strongResultActive
	strongResultExpired
)

// The terminal record retains the original pending epoch explicitly: rewriting
// the result changes its KV epoch. An active outcome's public epoch is the
// result entry's new epoch, shared with the promotion transaction.
type strongOutcomeRecord struct {
	AttemptID    string            `codec:"a"`
	Name         string            `codec:"n"`
	PID          string            `codec:"p"`
	Phase        strongResultPhase `codec:"s"`
	PendingEpoch uint64            `codec:"e"`
	Reason       string            `codec:"r,omitempty"`
	Missing      []pid.NodeID      `codec:"m,omitempty"`
	RetainUntil  int64             `codec:"u,omitempty"`
}

// completeStrongResultOps supplies BOTH attempt fences. The caller must combine
// these ops with its winning promote/expire operations in one transaction.
// It cannot publish an outcome for a replacement name or enlarge its budget.
func completeStrongResultOps(pending, result kvapi.Entry, phase strongResultPhase, reason string, missing []pid.NodeID, retainUntil int64) ([]kvapi.TxnOp, error) {
	if pending.Version == 0 || result.Version == 0 || pending.Epoch < result.Epoch || retainUntil <= 0 {
		return nil, fmt.Errorf("invalid Strong result transition identity")
	}
	header, err := decodePending(pending.Value)
	if err != nil {
		return nil, err
	}
	if header.AttemptID == "" || pending.Key != pendingKey(header.Name) || result.Key != strongResultKey(header.AttemptID) {
		return nil, fmt.Errorf("Strong result transition does not match pending attempt")
	}
	var reservation strongResultReservation
	if err := decodeInto(result.Value, &reservation); err != nil {
		return nil, err
	}
	if uint64(len(result.Key)) > reservation.Capacity || uint64(len(result.Value)) > reservation.Capacity-uint64(len(result.Key)) {
		return nil, fmt.Errorf("invalid Strong result reserved capacity")
	}
	var current strongOutcomeRecord
	if err := decodeInto(reservation.Data, &current); err != nil {
		return nil, err
	}
	if current.Phase != strongResultPending || current.AttemptID != header.AttemptID || current.Name != header.Name || current.PID != header.PID {
		return nil, fmt.Errorf("Strong result already terminal or owned by another attempt")
	}
	switch phase {
	case strongResultActive:
		if reason != "" || len(missing) != 0 {
			return nil, fmt.Errorf("active Strong result carries failure evidence")
		}
	case strongResultExpired:
		if reason != "deadline" && reason != strongRejectConflict && reason != "unreserve" {
			return nil, fmt.Errorf("invalid Strong terminal reason")
		}
		if reason == "deadline" && len(missing) == 0 {
			return nil, fmt.Errorf("Strong deadline result requires missing acknowledgement evidence")
		}
	default:
		return nil, fmt.Errorf("Strong result transition must be terminal")
	}
	if len(missing) > len(header.RequiredNodes) {
		return nil, fmt.Errorf("Strong result exceeds its participant set")
	}
	// Bound temporary participant work by the admitted serialized budget too.
	// The admission planner must reserve enough for the worst missing-node set.
	if uint64(len(header.RequiredNodes)) > reservation.Capacity {
		return nil, errStrongResultCapacity
	}
	remaining := reservation.Capacity - uint64(len(result.Key))
	for _, node := range missing {
		if uint64(len(node)) > remaining {
			return nil, errStrongResultCapacity
		}
		remaining -= uint64(len(node))
	}
	required := make(map[pid.NodeID]struct{}, len(header.RequiredNodes))
	for _, node := range header.RequiredNodes {
		required[node] = struct{}{}
	}
	for _, node := range missing {
		if _, ok := required[node]; !ok {
			return nil, fmt.Errorf("Strong result names an unrelated participant")
		}
	}
	// The untouched result reservation retains the creation index. A pending
	// header rewrite may advance its index without changing the attempt. The
	// transaction still fences the current pending version and exact result.
	current.Phase, current.PendingEpoch, current.Reason, current.RetainUntil = phase, result.Epoch, reason, retainUntil
	current.Missing = slices.Clone(missing)
	slices.Sort(current.Missing)
	current.Missing = slices.Compact(current.Missing)
	data, err := encode(current)
	if err != nil {
		return nil, err
	}
	if uint64(len(data)) > reservation.Capacity-uint64(len(result.Key)) {
		return nil, errStrongResultCapacity
	}
	reservation.Data = data
	body, err := encode(reservation)
	if err != nil {
		return nil, err
	}
	if uint64(len(body)) > reservation.Capacity-uint64(len(result.Key)) {
		return nil, errStrongResultCapacity
	}
	return []kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: pending.Key, Expect: pending.Version},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondVersion, Key: result.Key, Expect: result.Version, Value: body},
	}, nil
}

// planStrongResultCapacity reserves for the largest terminal representation,
// including every required node, the longest reason, and full-width counters.
// It returns the pending payload; callers still must atomically reserve capacity
// with pending creation. maxBytes bounds one record's serialized key + value.
func planStrongResultCapacity(header pendingHeader, maxBytes uint64) (uint64, []byte, error) {
	if header.AttemptID == "" || header.Name == "" || header.PID == "" || maxBytes == 0 {
		return 0, nil, fmt.Errorf("invalid Strong result admission identity or limit")
	}
	if err := validateRequiredIncarnations(header); err != nil {
		return 0, nil, err
	}
	// Bound input work before encoding. Empty node strings also consume encoded
	// metadata, so count them independently of their aggregate content length.
	if uint64(len(header.RequiredNodes)) > maxBytes {
		return 0, nil, errStrongResultTooLarge
	}
	remaining := maxBytes
	consume := func(value string) bool {
		if uint64(len(value)) > remaining {
			return false
		}
		remaining -= uint64(len(value))
		return true
	}
	for _, value := range []string{strongResultPrefix, header.AttemptID, header.AttemptID, header.Name, header.PID, strongRejectConflict} {
		if !consume(value) {
			return 0, nil, errStrongResultTooLarge
		}
	}
	for _, node := range header.RequiredNodes {
		if !consume(node) {
			return 0, nil, errStrongResultTooLarge
		}
	}
	worst := strongOutcomeRecord{
		AttemptID: header.AttemptID, Name: header.Name, PID: header.PID,
		Phase: strongResultExpired, PendingEpoch: ^uint64(0), Reason: strongRejectConflict,
		Missing: header.RequiredNodes, RetainUntil: int64(^uint64(0) >> 1),
	}
	data, err := encode(worst)
	if err != nil {
		return 0, nil, err
	}
	value, err := encode(strongResultReservation{Capacity: ^uint64(0), Data: data})
	if err != nil {
		return 0, nil, err
	}
	keyBytes := uint64(len(strongResultPrefix)) + uint64(len(header.AttemptID))
	if keyBytes > maxBytes || uint64(len(value)) > maxBytes-keyBytes {
		return 0, nil, errStrongResultTooLarge
	}
	capacity := keyBytes + uint64(len(value))
	initial, err := encode(strongOutcomeRecord{AttemptID: header.AttemptID, Name: header.Name, PID: header.PID})
	if err != nil {
		return 0, nil, err
	}
	return capacity, initial, nil
}

// terminalResultOps adds the result write to a winning name transaction. The
// caller-supplied pending version must still be current at commit.
func (st *strongState) terminalResultOps(name string, epoch, version uint64, header pendingHeader, phase strongResultPhase, reason string, missing []pid.NodeID) ([]kvapi.TxnOp, error) {
	if header.AttemptID == "" {
		return nil, fmt.Errorf("Strong completion requires an attempt identity")
	}
	result, err := st.svc.engine.Get(strongResultKey(header.AttemptID))
	if err != nil {
		return nil, err
	}
	body, err := encode(header)
	if err != nil {
		return nil, err
	}
	return completeStrongResultOps(kvapi.Entry{Key: pendingKey(name), Value: body, Epoch: epoch, Version: version}, result, phase, reason, missing, st.clock().Add(st.resultPolicy.Retention).UnixNano())
}
