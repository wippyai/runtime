// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"fmt"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"slices"
	"strings"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func decodeStrongOutcome(entry kvapi.Entry, maxBytes uint64) (strongOutcomeRecord, error) {
	var outcome strongOutcomeRecord
	if entry.Version == 0 || !strings.HasPrefix(entry.Key, strongResultPrefix) || len(entry.Key) == len(strongResultPrefix) || uint64(len(entry.Key)) > maxBytes || uint64(len(entry.Value)) > maxBytes-uint64(len(entry.Key)) {
		return outcome, fmt.Errorf("invalid Strong result entry or size")
	}
	var reservation strongResultReservation
	if err := decodeInto(entry.Value, &reservation); err != nil {
		return outcome, err
	}
	if reservation.Capacity > maxBytes || uint64(len(entry.Key)) > reservation.Capacity || uint64(len(entry.Value)) > reservation.Capacity-uint64(len(entry.Key)) {
		return outcome, fmt.Errorf("invalid Strong result reserved size")
	}
	if err := decodeInto(reservation.Data, &outcome); err != nil {
		return outcome, err
	}
	if outcome.AttemptID == "" || entry.Key != strongResultKey(outcome.AttemptID) || outcome.Name == "" {
		return outcome, fmt.Errorf("invalid Strong result identity")
	}
	if _, err := pid.ParsePID(outcome.PID); err != nil {
		return outcome, fmt.Errorf("invalid Strong result owner: %w", err)
	}
	switch outcome.Phase {
	case strongResultPending:
		if outcome.PendingEpoch != 0 || outcome.Reason != "" || len(outcome.Missing) != 0 || outcome.RetainUntil != 0 {
			return outcome, fmt.Errorf("pending Strong result carries terminal evidence")
		}
	case strongResultActive:
		if outcome.Reason != "" || len(outcome.Missing) != 0 || outcome.RetainUntil <= 0 {
			return outcome, fmt.Errorf("invalid active Strong result")
		}
	case strongResultExpired:
		if outcome.RetainUntil <= 0 || (outcome.Reason != "deadline" && outcome.Reason != strongRejectConflict && outcome.Reason != "unreserve") || (outcome.Reason == "deadline" && len(outcome.Missing) == 0) {
			return outcome, fmt.Errorf("invalid expired Strong result")
		}
		if !slices.IsSorted(outcome.Missing) {
			return outcome, fmt.Errorf("unsorted Strong result evidence")
		}
		for i, node := range outcome.Missing {
			if node == "" || (i != 0 && node == outcome.Missing[i-1]) {
				return outcome, fmt.Errorf("invalid Strong missing participant")
			}
		}
	default:
		return outcome, fmt.Errorf("unknown Strong result phase")
	}
	return outcome, nil
}

// readStrongOutcome uses the supplied authoritative reader and the exact attempt
// key. Absence remains unavailable evidence; it is never translated to timeout.
// RetainUntil governs GC eligibility, not validity of a still-present result.
func readStrongOutcome(read func(string) (kvapi.Entry, error), attempt, name, owner string, maxBytes uint64) (kvapi.Entry, strongOutcomeRecord, error) {
	if read == nil || attempt == "" || name == "" || owner == "" || uint64(len(strongResultPrefix)) > maxBytes || uint64(len(attempt)) > maxBytes-uint64(len(strongResultPrefix)) {
		return kvapi.Entry{}, strongOutcomeRecord{}, fmt.Errorf("invalid Strong outcome lookup")
	}
	entry, err := read(strongResultKey(attempt))
	if err != nil {
		return kvapi.Entry{}, strongOutcomeRecord{}, err
	}
	outcome, err := decodeStrongOutcome(entry, maxBytes)
	if err != nil {
		return entry, outcome, err
	}
	if outcome.AttemptID != attempt || outcome.Name != name || outcome.PID != owner {
		return entry, outcome, fmt.Errorf("Strong outcome belongs to a different attempt or owner")
	}
	return entry, outcome, nil
}

// expiredStrongResultOps considers terminal evidence retention only. It cannot
// expire a pending claim, remove an active name, or credit a different record.
func expiredStrongResultOps(read func(string) (kvapi.Entry, error), entry kvapi.Entry, now int64, maxBytes uint64) ([]kvapi.TxnOp, error) {
	outcome, err := decodeStrongOutcome(entry, maxBytes)
	if err != nil {
		return nil, err
	}
	if outcome.Phase == strongResultPending || outcome.RetainUntil > now {
		return nil, nil
	}
	return releaseStrongResultOps(read, entry)
}

func (st *strongState) attemptOutcome(ctx context.Context, name string, owner pid.PID, attempt string) (globalapi.RegisterOutcome, error) {
	entry, result, err := readStrongOutcome(func(key string) (kvapi.Entry, error) { return st.svc.getContext(ctx, key) }, attempt, name, owner.String(), st.resultPolicy.RecordBytes)
	if err != nil {
		return globalapi.RegisterOutcome{}, err
	}
	switch result.Phase {
	case strongResultActive:
		return globalapi.RegisterOutcome{PID: owner, Epoch: entry.Epoch, State: globalapi.RegisterStateActive}, nil
	case strongResultExpired:
		out := globalapi.RegisterOutcome{Epoch: result.PendingEpoch, State: globalapi.RegisterStateExpired}
		switch result.Reason {
		case strongRejectConflict:
			return out, &globalapi.StrongConflictError{Name: name, Epoch: result.PendingEpoch, Reason: result.Reason}
		case "deadline":
			return out, &globalapi.StrongRegistrationTimeoutError{Name: name, Epoch: result.PendingEpoch, MissingAcks: result.Missing}
		case "unreserve":
			// Explicit withdrawal is not deadline expiry.
			return globalapi.RegisterOutcome{Epoch: result.PendingEpoch}, globalapi.ErrStrongRegistrationWithdrawn
		}
	}
	return globalapi.RegisterOutcome{}, globalapi.ErrNotAvailable
}

func (st *strongState) finalizeAttempt(ctx context.Context, name string, owner pid.PID, attempt string, out globalapi.RegisterOutcome) (globalapi.RegisterOutcome, error) {
	// Exact active notifications already captured committed success. A later
	// disconnection or reclamation must not turn that known result into failure.
	if out.State == globalapi.RegisterStateActive {
		return st.finalize(name, owner, out)
	}
	return st.attemptOutcome(ctx, name, owner, attempt)
}
