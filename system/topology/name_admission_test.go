// SPDX-License-Identifier: MPL-2.0

package topology_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/system/topology"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

func TestLocalAndEventualAdmitSameNameIndependently(t *testing.T) {
	local := topology.NewPIDRegistry()
	eventualReg := eventual.NewService(eventual.Config{LocalNodeID: "node-a"})
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
		if localErr != nil || eventualErr != nil {
			t.Fatalf("%s: independent registrations failed: local=%v eventual=%v", name, localErr, eventualErr)
		}
		if p, ok := local.LookupLocal(name); !ok || !p.Equal(localPID) {
			t.Fatalf("%s: LOCAL binding lost: %v, %v", name, p, ok)
		}
		res, err := eventualReg.Lookup(context.Background(), name)
		if err != nil || !res.Found || !res.PID.Equal(eventualPID) {
			t.Fatalf("%s: EVENTUAL binding lost: result=%+v err=%v", name, res, err)
		}
		if p, ok := local.Lookup(name); !ok || !p.Equal(eventualPID) {
			t.Fatalf("%s: composed lookup did not prefer EVENTUAL: %v, %v", name, p, ok)
		}
	}
}
