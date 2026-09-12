// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/wippyai/runtime/api/pid"
	topapi "github.com/wippyai/runtime/api/topology"
	local "github.com/wippyai/runtime/system/topology"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

type admissionScope struct{ registry *Service }

func (s admissionScope) NameReady() bool { return true }
func (s admissionScope) LookupOther(name string) (pid.PID, bool) {
	p, found := s.registry.IsStrongReserved(name)
	runtime.Gosched()
	return p, found
}

func TestKVStrongAdmissionCannotRaceWeakerClaim(t *testing.T) {
	for _, scope := range []string{"local", "eventual"} {
		t.Run(scope, func(t *testing.T) {
			guard := &topapi.NameGuard{}
			r := newStrongReg(t, []pid.NodeID{"node-1"}, 0, nil)
			r.strong.nameGuard = guard
			r.ready.Store(true)
			weakPID, strongPID := mkPID("node-1", "weak"), mkPID("node-1", "strong")
			var register func(string) error
			var lookup func(string) (pid.PID, bool)
			if scope == "local" {
				weak := local.NewPIDRegistry(local.WithNameGuard(guard), local.WithGlobalRegistry(r))
				register = func(name string) error { _, err := weak.Register(name, weakPID); return err }
				lookup = weak.LookupLocal
			} else {
				weak := eventual.NewService(eventual.Config{LocalNodeID: "node-1", NameGuard: guard, CrossScope: admissionScope{r}})
				register = func(name string) error { _, err := weak.Register(name, weakPID); return err }
				lookup = func(name string) (pid.PID, bool) {
					res, _ := weak.Lookup(context.Background(), name)
					return res.PID, res.Found
				}
			}
			r.strong.localConflict = func(name string, _ pid.PID) (pid.PID, bool) {
				p, found := lookup(name)
				runtime.Gosched()
				return p, found
			}
			for i := range 500 {
				name := fmt.Sprintf("claim.%d", i)
				start, done := make(chan struct{}), make(chan struct{})
				registered := make(chan error, 1)
				go func() { <-start; r.strong.attest(name, 1, strongPID, []pid.NodeID{"node-1"}); close(done) }()
				go func() { <-start; registered <- register(name) }()
				close(start)
				<-done
				err := <-registered
				_, acked := r.IsStrongReserved(name)
				if acked && err == nil {
					t.Fatal("Strong exclusion and conflicting weaker claim both succeeded")
				}
				if !acked && err != nil {
					t.Fatalf("neither contender admitted: %v", err)
				}
			}
		})
	}
}
