// SPDX-License-Identifier: MPL-2.0

package global

import (
	"context"
	"sort"
	"testing"

	hraft "github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/api/pid"
)

func registerInFSM(t *testing.T, fsm *FSM, name string, p pid.PID, index uint64) {
	t.Helper()
	cmd := &Command{Type: CmdRegister, Name: name, PID: p, NodeID: p.Node}
	data, err := EncodeCommand(cmd)
	require.NoError(t, err)
	resp := fsm.Apply(&hraft.Log{Data: data, Index: index})
	rr, ok := resp.(*RegisterResult)
	require.True(t, ok)
	require.NoError(t, rr.Err)
}

func newServiceWithState(t *testing.T) *Service {
	t.Helper()
	s := &Service{fsm: NewFSM()}
	s.ready = true
	return s
}

func TestService_Lookup_Equivalence_Plain(t *testing.T) {
	s := newServiceWithState(t)
	p := pid.PID{Node: "node-a", Host: "h", UniqID: "p1"}
	registerInFSM(t, s.fsm, "svc.alpha", p, 1)

	// Direct state read.
	statePID, stateOK := s.fsm.State().Lookup("svc.alpha")
	require.True(t, stateOK)

	// New unified API, no options.
	res, err := s.Lookup(context.Background(), "svc.alpha")
	require.NoError(t, err)
	assert.True(t, res.Found)
	assert.Equal(t, statePID, res.PID)
	assert.Nil(t, res.NamesForPID)
}

func TestService_Lookup_Equivalence_ByPID(t *testing.T) {
	s := newServiceWithState(t)
	p := pid.PID{Node: "node-a", Host: "h", UniqID: "owner"}
	other := pid.PID{Node: "node-a", Host: "h", UniqID: "other"}
	registerInFSM(t, s.fsm, "svc.one", p, 1)
	registerInFSM(t, s.fsm, "svc.two", p, 2)
	registerInFSM(t, s.fsm, "svc.three", p, 3)
	registerInFSM(t, s.fsm, "svc.elsewhere", other, 4)

	stateNames := s.fsm.State().LookupByPID(p)
	sort.Strings(stateNames)
	require.Equal(t, []string{"svc.one", "svc.three", "svc.two"}, stateNames)

	res, err := s.Lookup(context.Background(), "", global.ByPID(p))
	require.NoError(t, err)
	assert.True(t, res.Found)
	assert.Equal(t, p, res.PID)
	got := append([]string(nil), res.NamesForPID...)
	sort.Strings(got)
	assert.Equal(t, stateNames, got)
}

func TestService_Lookup_NotFound(t *testing.T) {
	s := newServiceWithState(t)
	res, err := s.Lookup(context.Background(), "missing")
	require.NoError(t, err)
	assert.False(t, res.Found)
	assert.Equal(t, pid.PID{}, res.PID)
}

func TestService_Lookup_ByPID_EmptyPID(t *testing.T) {
	s := newServiceWithState(t)
	res, err := s.Lookup(context.Background(), "", global.ByPID(pid.PID{Node: "ghost"}))
	require.NoError(t, err)
	assert.False(t, res.Found)
	assert.Empty(t, res.NamesForPID)
}

func TestService_Lookup_PropertyParity(t *testing.T) {
	s := newServiceWithState(t)

	ops := []struct {
		name  string
		p     pid.PID
		index uint64
	}{
		{"a", pid.PID{Node: "n1", Host: "h", UniqID: "a"}, 1},
		{"b", pid.PID{Node: "n1", Host: "h", UniqID: "b"}, 2},
		{"c", pid.PID{Node: "n2", Host: "h", UniqID: "c"}, 3},
		{"d", pid.PID{Node: "n2", Host: "h", UniqID: "a"}, 4},
		{"e", pid.PID{Node: "n3", Host: "h", UniqID: "e"}, 5},
	}
	for _, op := range ops {
		registerInFSM(t, s.fsm, op.name, op.p, op.index)
	}

	for _, op := range ops {
		stateP, stateOK := s.fsm.State().Lookup(op.name)
		res, err := s.Lookup(context.Background(), op.name)
		require.NoError(t, err)
		assert.Equal(t, stateOK, res.Found, "op=%s", op.name)
		assert.Equal(t, stateP, res.PID, "op=%s", op.name)
	}

	for _, op := range ops {
		stateNames := s.fsm.State().LookupByPID(op.p)
		resPID, err := s.Lookup(context.Background(), "", global.ByPID(op.p))
		require.NoError(t, err)
		sort.Strings(stateNames)
		got := append([]string(nil), resPID.NamesForPID...)
		sort.Strings(got)
		assert.Equal(t, stateNames, got, "byPID=%s", op.p.UniqID)
		assert.Equal(t, len(stateNames) > 0, resPID.Found, "byPID=%s", op.p.UniqID)
	}
}
