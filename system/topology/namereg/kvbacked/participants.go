// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// The roster is naming authority, not a gossip/liveness view. A participant
// must remain here until it has stopped admitting weaker names and every such
// binding is gone. This implementation deliberately has no automatic removal:
// a crashed or partitioned node cannot be safely retired from a gossip hint.
const participantsKey = registryPrefix + "participants"

const participantActivationTimeout = 10 * time.Second

type participantsValue struct {
	Nodes []participantEntry `codec:"n"`
}

type participantEntry struct {
	Node       pid.NodeID `codec:"n"`
	Activation string     `codec:"a"`
}

func (v participantsValue) requiredNodes() []pid.NodeID {
	out := make([]pid.NodeID, len(v.Nodes))
	for i, entry := range v.Nodes {
		out[i] = entry.Node
	}
	return out
}

func (v participantsValue) hasActivation(node pid.NodeID, activation string) bool {
	for _, entry := range v.Nodes {
		if entry.Node == node {
			return entry.Activation == activation
		}
	}
	return false
}

// decodeParticipants rejects noncanonical data so two nodes never interpret
// the same committed roster differently.
func decodeParticipants(data []byte) (participantsValue, error) {
	var roster participantsValue
	if err := decodeInto(data, &roster); err != nil {
		return roster, err
	}
	if len(roster.Nodes) == 0 {
		return roster, fmt.Errorf("empty naming participant roster")
	}
	for i, entry := range roster.Nodes {
		if entry.Node == "" || entry.Activation == "" ||
			(i > 0 && roster.Nodes[i-1].Node >= entry.Node) {
			return roster, fmt.Errorf("noncanonical naming participant roster")
		}
	}
	return roster, nil
}

func (st *strongState) readParticipants() (participantsValue, kvapi.Version, bool, error) {
	entry, err := st.svc.engine.Get(participantsKey)
	if errors.Is(err, kvapi.ErrKeyNotFound) {
		return participantsValue{}, 0, false, nil
	}
	if err != nil {
		return participantsValue{}, 0, false, fmt.Errorf("read naming participants: %w", err)
	}
	roster, err := decodeParticipants(entry.Value)
	if err != nil {
		return participantsValue{}, 0, false, fmt.Errorf("registry record %q: %w", participantsKey, err)
	}
	return roster, entry.Version, true, nil
}

// enroll commits this node's participation before any LOCAL/EVENTUAL admission
// opens. A same-name Strong pending races the enrollment through one KV key:
// either its roster-version check wins first, or its required set includes us.
// The caller must seed from a coherent local publication after enrollment is
// locally visible, catching every pending attempt that won the first order.
func (st *strongState) enroll(ctx context.Context) error {
	activation, err := newStrongAttempt()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, participantActivationTimeout)
	defer cancel()
	var submitErr error
	contextErr := func() error {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			if submitErr != nil {
				return fmt.Errorf("naming participant activation timed out (last submission: %w): %w", submitErr, ctx.Err())
			}
			return fmt.Errorf("naming participant activation timed out: %w", ctx.Err())
		}
		return ctx.Err()
	}
	for {
		select {
		case <-ctx.Done():
			return contextErr()
		default:
		}
		roster, version, exists, err := st.readParticipants()
		if err != nil {
			return err
		}
		next := append([]participantEntry(nil), roster.Nodes...)
		replaced := false
		for i := range next {
			if next[i].Node == st.svc.selfNode {
				next[i].Activation = activation
				replaced = true
				break
			}
		}
		if !replaced {
			next = append(next, participantEntry{Node: st.svc.selfNode, Activation: activation})
			sort.Slice(next, func(i, j int) bool { return next[i].Node < next[j].Node })
		}
		value, err := encode(participantsValue{Nodes: next})
		if err != nil {
			return err
		}
		condition := kvapi.CondAbsent
		if exists {
			condition = kvapi.CondVersion
		}
		ops := []kvapi.TxnOp{
			{Kind: kvapi.TxnPut, Cond: condition, Key: participantsKey, Expect: version, Value: value},
		}
		// Raft's Apply timeout only bounds enqueue, not Future.Error(). Wait
		// outside the engine call so startup can close admission on deadline.
		// Never issue a second submission while the first is unresolved.
		type txnResult struct {
			err       error
			committed bool
		}
		result := make(chan txnResult, 1)
		go func() {
			committed, err := st.svc.engine.Txn(ops)
			result <- txnResult{err: err, committed: committed}
		}()
		var committed bool
		select {
		case <-ctx.Done():
			return contextErr()
		case res := <-result:
			committed, err = res.committed, res.err
		}
		if err != nil {
			// A leader change can interrupt submission. The write may even have
			// committed with its reply lost; retrying the same node activation is
			// safe because admission has not opened yet.
			submitErr = err
		}
		if err == nil && committed {
			break
		}
		// Another enrollment won. Read its publication and retry without ever
		// opening admission against an assumed participant set.
		select {
		case <-ctx.Done():
			return contextErr()
		case <-time.After(10 * time.Millisecond):
		}
	}
	for {
		entries, _, err := st.svc.localRead.ReadLocalSnapshot([]string{participantsKey})
		if err != nil {
			return fmt.Errorf("observe naming participation: %w", err)
		}
		if entry, ok := entries[participantsKey]; ok {
			roster, err := decodeParticipants(entry.Value)
			if err != nil {
				return fmt.Errorf("registry record %q: %w", participantsKey, err)
			}
			if roster.hasActivation(st.svc.selfNode, activation) {
				st.activation = activation
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return contextErr()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
