// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"context"
	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	local "github.com/wippyai/runtime/system/topology"
	"github.com/wippyai/runtime/system/topology/namereg/kvbacked"
	"sync"
	"testing"
	"time"
)

// Characterization: changing the authority's discovery view does not revoke
// naming admission on the removed client. No client process is killed here.
func TestCharacterizationMembershipPruningDoesNotFenceClient(t *testing.T) {
	if testing.Short() {
		t.Skip("multinode characterization")
	}
	c := NewCluster(t, 3)
	clientID := "suspected-client"
	client := c.newClientRegistry(t, clientID)
	clientNames := local.NewPIDRegistry(local.WithGlobalRegistry(client))
	oldOwner := pid.PID{Node: clientID, Host: "process", UniqID: "old"}
	if _, err := clientNames.Register("name", oldOwner); err != nil {
		t.Fatal(err)
	}
	members := []pid.NodeID{clientID}
	for _, node := range c.Nodes() {
		members = append(members, node.ID)
	}
	var mu sync.Mutex
	inventory := func() []pid.NodeID { mu.Lock(); defer mu.Unlock(); return append([]pid.NodeID(nil), members...) }
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
		reg.ConfigureStrong(kvbacked.StrongDeps{Membership: inventory, IsLeader: node.Raft.IsLeader, Deadline: 10 * time.Second})
		if err := reg.StartReconciler(ctx); err != nil {
			t.Fatal(err)
		}
		regs[node.ID] = reg
	}
	leader := c.Leader()
	newOwner := pid.PID{Node: leader.ID, Host: "process", UniqID: "new"}
	result := make(chan error, 1)
	go func() {
		out, err := regs[leader.ID].RegisterScope(ctx, "name", newOwner, globalapi.Strong)
		if err == nil && out.State != globalapi.RegisterStateActive {
			err = globalapi.ErrNotAvailable
		}
		result <- err
	}()
	if !waitReserved(t, regs[leader.ID], "name", 3*time.Second) {
		cancel()
		<-result
		t.Fatal("pending exclusion not observed")
	}
	// Simulate the authority-side gossip view dropping the client; the client
	// remains alive and has received no retirement or exclusion protocol.
	mu.Lock()
	members = members[1:]
	mu.Unlock()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		cancel()
		<-result
		t.Fatal("survivor promotion did not occur")
	}
	held, found := clientNames.LookupLocal("name")
	if !found || !held.Equal(oldOwner) {
		t.Fatal("fixture unexpectedly retired client binding")
	}
	if !client.NameReady() {
		t.Fatal("fixture unexpectedly closed client admission")
	}
	t.Log("Strong promoted after participant pruning while removed client stayed name-ready and retained its conflicting LOCAL binding")
}

// A fixed pending set alone cannot fix retirement: fresh reservations also
// need an authoritative participant inventory independent of discovery.
func TestCharacterizationNewStrongOmitsLiveUndiscoveredClient(t *testing.T) {
	if testing.Short() {
		t.Skip("multinode characterization")
	}
	c := NewCluster(t, 3)
	clientID := "suspected-client"
	client := c.newClientRegistry(t, clientID)
	clientNames := local.NewPIDRegistry(local.WithGlobalRegistry(client))
	oldOwner := pid.PID{Node: clientID, Host: "process", UniqID: "old"}
	if _, err := clientNames.Register("name", oldOwner); err != nil {
		t.Fatal(err)
	}
	members := []pid.NodeID{clientID}
	for _, node := range c.Nodes() {
		members = append(members, node.ID)
	}
	var mu sync.Mutex
	inventory := func() []pid.NodeID { mu.Lock(); defer mu.Unlock(); return append([]pid.NodeID(nil), members...) }
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
		reg.ConfigureStrong(kvbacked.StrongDeps{Membership: inventory, IsLeader: node.Raft.IsLeader, Deadline: 10 * time.Second})
		if err := reg.StartReconciler(ctx); err != nil {
			t.Fatal(err)
		}
		regs[node.ID] = reg
	}
	leader := c.Leader()
	newOwner := pid.PID{Node: leader.ID, Host: "process", UniqID: "new"}
	// The client disappears from discovery before reservation creation. This
	// bypasses pending-set pruning entirely: the initial set already omits it.
	mu.Lock()
	members = members[1:]
	mu.Unlock()
	result := make(chan error, 1)
	go func() {
		out, err := regs[leader.ID].RegisterScope(ctx, "name", newOwner, globalapi.Strong)
		if err == nil && out.State != globalapi.RegisterStateActive {
			err = globalapi.ErrNotAvailable
		}
		result <- err
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		cancel()
		<-result
		t.Fatal("survivor promotion did not occur")
	}
	held, found := clientNames.LookupLocal("name")
	if !found || !held.Equal(oldOwner) {
		t.Fatal("fixture unexpectedly retired client binding")
	}
	if !client.NameReady() {
		t.Fatal("fixture unexpectedly closed client admission")
	}
	t.Log("New Strong omitted an undiscovered live client and promoted while its conflicting LOCAL binding and admission remained active")
}
