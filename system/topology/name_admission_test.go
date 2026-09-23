// SPDX-License-Identifier: MPL-2.0

package topology_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/system/topology"
	"github.com/wippyai/runtime/system/topology/namereg/admission"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

type localClaimChecker struct{ local *topology.PIDRegistry }

func (c localClaimChecker) LookupOther(name string, _ pid.PID) (pid.PID, bool, error) {
	p, ok := c.local.LookupLocal(name)
	return p, ok, nil
}

func (localClaimChecker) NameReady() bool { return true }

func TestLocalAndEventualCannotBothAdmitSameName(t *testing.T) {
	gate := &admission.Coordinator{}
	local := topology.NewPIDRegistry(topology.WithAdmissionCoordinator(gate))
	eventualReg := eventual.NewService(eventual.Config{
		LocalNodeID: "node-a", Admission: gate, CrossScope: localClaimChecker{local: local},
	})
	local.SetEventualRegistry(eventualReg)
	localPID := pid.PID{Node: "node-a", Host: "host", UniqID: "local"}
	eventualPID := pid.PID{Node: "node-a", Host: "host", UniqID: "eventual"}

	for i := range 100 {
		name := fmt.Sprintf("race-%d", i)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var localErr, eventualErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, localErr = local.Register(name, localPID)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, eventualErr = eventualReg.Register(name, eventualPID)
		}()
		close(start)
		wg.Wait()
		if (localErr == nil) == (eventualErr == nil) {
			t.Fatalf("%s: local=%v eventual=%v; want exactly one owner", name, localErr, eventualErr)
		}
		if p, ok := local.LookupLocal(name); ok && p.Equal(localPID) {
			res, err := eventualReg.Lookup(context.Background(), name)
			if err != nil || res.Found {
				t.Fatalf("%s: EVENTUAL shadowed LOCAL: result=%+v err=%v", name, res, err)
			}
		}
	}
}
