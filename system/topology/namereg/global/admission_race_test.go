// SPDX-License-Identifier: MPL-2.0

package global

import (
	"context"
	"runtime"
	"testing"

	"github.com/wippyai/runtime/api/pid"
	topapi "github.com/wippyai/runtime/api/topology"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	local "github.com/wippyai/runtime/system/topology"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

type admissionRegistry struct{ *Service }

func (s admissionRegistry) NameReady() bool { return true }
func (s admissionRegistry) Lookup(context.Context, string, ...globalapi.LookupOption) (globalapi.LookupResult, error) {
	return globalapi.LookupResult{}, nil
}
func (s admissionRegistry) LookupOther(name string) (pid.PID, bool) {
	p, found := s.IsStrongReserved(name)
	runtime.Gosched() // exercise the gap between observing absence and publishing
	return p, found
}

func TestStrongAdmissionCannotRaceWeakerClaim(t *testing.T) {
	for _, scope := range []string{"local", "eventual"} {
		t.Run(scope, func(t *testing.T) {
			for range 500 {
				guard := &topapi.NameGuard{}
				strong := &Service{nameGuard: guard, strongExclusions: make(map[string]strongExclusion)}
				view := admissionRegistry{strong}
				weakPID := makePID("node", "host", "weak")
				strongPID := makePID("node", "host", "strong")
				var register func() error
				var lookup func() (pid.PID, bool)
				if scope == "local" {
					weak := local.NewPIDRegistry(local.WithNameGuard(guard), local.WithGlobalRegistry(view))
					register = func() error { _, err := weak.Register("name", weakPID); return err }
					lookup = func() (pid.PID, bool) { return weak.LookupLocal("name") }
				} else {
					weak := eventual.NewService(eventual.Config{LocalNodeID: "node", NameGuard: guard, CrossScope: view})
					register = func() error { _, err := weak.Register("name", weakPID); return err }
					lookup = func() (pid.PID, bool) { r, _ := weak.Lookup(context.Background(), "name"); return r.PID, r.Found }
				}
				start := make(chan struct{})
				claimed := make(chan bool, 1)
				registered := make(chan error, 1)
				go func() {
					<-start
					claimed <- strong.reserveCheckAndLatch("name", strongPID, 1, func() (pid.PID, bool) { p, found := lookup(); runtime.Gosched(); return p, found })
				}()
				go func() { <-start; registered <- register() }()
				close(start)
				acked, err := <-claimed, <-registered
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
