// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"context"
	"errors"
	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/topology/namereg/kvbacked"
	"slices"
	"testing"
	"time"
)

// Characterization, not acceptance: production currently includes all gossip
// nodes in Strong's required set, but a forwarding client has no exclusion feed.
func TestCharacterizationStrongCannotCompleteWithUnenrolledClient(t *testing.T) {
	if testing.Short() {
		t.Skip("multinode characterization")
	}
	c := NewCluster(t, 3)
	clientID := "forwarding-client"
	_ = c.newClientRegistry(t, clientID)
	members := []pid.NodeID{clientID}
	for _, node := range c.Nodes() {
		members = append(members, node.ID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	regs := make(map[string]*kvbacked.Service)
	defer func() {
		cancel()
		for _, reg := range regs {
			if err := reg.StopReconciler(context.Background()); err != nil {
				t.Error(err)
			}
		}
	}()
	for _, node := range c.Nodes() {
		reg := kvbacked.NewService(node.KV, node.ID, nil, nil)
		reg.ConfigureStrong(kvbacked.StrongDeps{Membership: func() []pid.NodeID { return members }, IsLeader: node.Raft.IsLeader, Deadline: time.Second})
		if err := reg.StartReconciler(ctx); err != nil {
			t.Fatal(err)
		}
		regs[node.ID] = reg
	}
	leader := c.Leader()
	out, err := regs[leader.ID].RegisterScope(context.Background(), "requires-client", pid.PID{Node: leader.ID, Host: "process", UniqID: "owner"}, globalapi.Strong)
	var timeout *globalapi.StrongRegistrationTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("expected missing-client timeout; got outcome=%+v err=%v", out, err)
	}
	if !slices.Contains(timeout.MissingAcks, clientID) {
		t.Fatalf("missing-client diagnosis lost: %+v", timeout)
	}
	t.Logf("confirmed missing participant %s; missing=%v", clientID, timeout.MissingAcks)
}
