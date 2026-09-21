// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"errors"
	"testing"

	"github.com/wippyai/runtime/api/pid"
)

type failingCrossScope struct{ err error }

func (c failingCrossScope) LookupOther(string, pid.PID) (pid.PID, bool, error) {
	return pid.PID{}, false, c.err
}

func (failingCrossScope) NameReady() bool { return true }

func TestRegisterCrossScopeReadFailureLeavesNoIntent(t *testing.T) {
	wantErr := errors.New("registry read failed")
	s := NewService(Config{LocalNodeID: "node-a", CrossScope: failingCrossScope{err: wantErr}})
	p := pid.PID{Node: "node-a", Host: "host", UniqID: "owner"}
	_, err := s.Register("blocked", p)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Register error = %v, want %v", err, wantErr)
	}
	if _, ok := s.state.Lookup("blocked"); ok {
		t.Fatal("failed read created an EVENTUAL binding")
	}
	if len(s.owned) != 0 || s.queue.Depth() != 0 {
		t.Fatalf("failed read retained intent: owned=%d queued=%d", len(s.owned), s.queue.Depth())
	}
}
