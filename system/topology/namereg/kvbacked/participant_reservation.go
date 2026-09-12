// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"fmt"
	"sort"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// reservationOps prepares an atomic reservation against the committed naming
// inventory. It never consults discovery. The inventory condition must remain
// in the same transaction as the pending put; a failed condition requires a
// fresh inventory read, not a retry of these stale operations.
//
// This internal foundation is not yet enabled by boot: enrollment snapshot
// installation and exact-incarnation acknowledgement validation must land first.
func (p *participantInventory) reservationOps(ctx context.Context, hdr pendingHeader) ([]kvapi.TxnOp, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owner, err := pid.ParsePID(hdr.PID)
	if err != nil || hdr.Name == "" || owner.Node == "" {
		return nil, fmt.Errorf("invalid participant reservation owner or name")
	}
	members, check, err := p.readSnapshot()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, ok := members[owner.Node]; !ok {
		return nil, fmt.Errorf("reservation owner node is not enrolled: %s", owner.Node)
	}
	hdr.NodeID = owner.Node
	hdr.RequiredNodes = make([]pid.NodeID, 0, len(members))
	for node := range members {
		hdr.RequiredNodes = append(hdr.RequiredNodes, node)
	}
	sort.Strings(hdr.RequiredNodes)
	hdr.RequiredIncarnations = members
	body, err := encode(hdr)
	if err != nil {
		return nil, err
	}
	return []kvapi.TxnOp{
		check,
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondAbsent, Key: activeKey(hdr.Name)},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: pendingKey(hdr.Name), Value: body},
	}, nil
}
