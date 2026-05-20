// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"context"
	"sort"
	"testing"

	hraft "github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/globalreg"
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

	// Legacy direct state read.
	legacyPID, legacyOK := s.fsm.State().Lookup("svc.alpha")
	require.True(t, legacyOK)

	// New unified API, no options.
	res, err := s.Lookup(context.Background(), "svc.alpha")
	require.NoError(t, err)
	assert.True(t, res.Found)
	assert.Equal(t, legacyPID, res.PID)
	assert.EqualValues(t, 0, res.FenceToken, "no WithFence → token must stay zero")
	assert.Nil(t, res.NamesForPID)
}

func TestService_Lookup_Equivalence_WithFence(t *testing.T) {
	s := newServiceWithState(t)
	p := pid.PID{Node: "node-a", Host: "h", UniqID: "p1"}
	registerInFSM(t, s.fsm, "svc.beta", p, 42)

	legacyPID, legacyToken, legacyOK := s.fsm.State().LookupWithFence("svc.beta")
	require.True(t, legacyOK)
	assert.EqualValues(t, 42, legacyToken)

	res, err := s.Lookup(context.Background(), "svc.beta", globalreg.WithFence())
	require.NoError(t, err)
	assert.True(t, res.Found)
	assert.Equal(t, legacyPID, res.PID)
	assert.Equal(t, legacyToken, res.FenceToken)
}

func TestService_Lookup_Equivalence_ByPID(t *testing.T) {
	s := newServiceWithState(t)
	p := pid.PID{Node: "node-a", Host: "h", UniqID: "owner"}
	other := pid.PID{Node: "node-a", Host: "h", UniqID: "other"}
	registerInFSM(t, s.fsm, "svc.one", p, 1)
	registerInFSM(t, s.fsm, "svc.two", p, 2)
	registerInFSM(t, s.fsm, "svc.three", p, 3)
	registerInFSM(t, s.fsm, "svc.elsewhere", other, 4)

	legacyNames := s.fsm.State().LookupByPID(p)
	sort.Strings(legacyNames)
	require.Equal(t, []string{"svc.one", "svc.three", "svc.two"}, legacyNames)

	res, err := s.Lookup(context.Background(), "", globalreg.ByPID(p))
	require.NoError(t, err)
	assert.True(t, res.Found)
	assert.Equal(t, p, res.PID)
	got := append([]string(nil), res.NamesForPID...)
	sort.Strings(got)
	assert.Equal(t, legacyNames, got)
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
	res, err := s.Lookup(context.Background(), "", globalreg.ByPID(pid.PID{Node: "ghost"}))
	require.NoError(t, err)
	assert.False(t, res.Found)
	assert.Empty(t, res.NamesForPID)
}

func TestService_Lookup_WithFenceNotReady(t *testing.T) {
	s := &Service{fsm: NewFSM()}
	res, err := s.Lookup(context.Background(), "svc.x", globalreg.WithFence())
	require.NoError(t, err)
	assert.False(t, res.Found)
	assert.EqualValues(t, 0, res.FenceToken)
}

func TestService_LegacyShims_AgreeWithUnified(t *testing.T) {
	s := newServiceWithState(t)
	p := pid.PID{Node: "node-a", Host: "h", UniqID: "owner"}
	registerInFSM(t, s.fsm, "svc.shim", p, 99)
	registerInFSM(t, s.fsm, "svc.also.shim", p, 100)

	resOld := s.LookupWithFence("svc.shim")
	resNew, err := s.Lookup(context.Background(), "svc.shim", globalreg.WithFence())
	require.NoError(t, err)
	assert.Equal(t, resOld.PID, resNew.PID)
	assert.Equal(t, resOld.FenceToken, resNew.FenceToken)
	assert.Equal(t, resOld.Found, resNew.Found)

	namesOld := s.LookupByPID(p)
	namesNew, err := s.Lookup(context.Background(), "", globalreg.ByPID(p))
	require.NoError(t, err)
	sort.Strings(namesOld)
	sortedNew := append([]string(nil), namesNew.NamesForPID...)
	sort.Strings(sortedNew)
	assert.Equal(t, namesOld, sortedNew)
}

func TestGlobalregValidateFence_Helper(t *testing.T) {
	s := newServiceWithState(t)
	p := pid.PID{Node: "node-a", Host: "h", UniqID: "owner"}
	registerInFSM(t, s.fsm, "svc.fence", p, 7)

	require.NoError(t, globalreg.ValidateFence(context.Background(), s, "svc.fence", 7))
	require.NoError(t, globalreg.ValidateFence(context.Background(), s, "svc.fence", 99),
		"token >= AppliedAt is valid")

	err := globalreg.ValidateFence(context.Background(), s, "svc.fence", 3)
	require.ErrorIs(t, err, globalreg.ErrStaleFence)

	err = globalreg.ValidateFence(context.Background(), s, "svc.missing", 1)
	require.ErrorIs(t, err, globalreg.ErrStaleFence,
		"missing name surfaces as stale-fence so callers cannot send to ghosts")
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
		legacyP, legacyOK := s.fsm.State().Lookup(op.name)
		res, err := s.Lookup(context.Background(), op.name)
		require.NoError(t, err)
		assert.Equal(t, legacyOK, res.Found, "op=%s", op.name)
		assert.Equal(t, legacyP, res.PID, "op=%s", op.name)

		legacyFP, legacyTok, legacyFOK := s.fsm.State().LookupWithFence(op.name)
		resF, err := s.Lookup(context.Background(), op.name, globalreg.WithFence())
		require.NoError(t, err)
		assert.Equal(t, legacyFOK, resF.Found, "op=%s with fence", op.name)
		assert.Equal(t, legacyFP, resF.PID, "op=%s with fence", op.name)
		assert.Equal(t, legacyTok, resF.FenceToken, "op=%s with fence", op.name)
	}

	for _, op := range ops {
		legacyNames := s.fsm.State().LookupByPID(op.p)
		resPID, err := s.Lookup(context.Background(), "", globalreg.ByPID(op.p))
		require.NoError(t, err)
		sort.Strings(legacyNames)
		got := append([]string(nil), resPID.NamesForPID...)
		sort.Strings(got)
		assert.Equal(t, legacyNames, got, "byPID=%s", op.p.UniqID)
		assert.Equal(t, len(legacyNames) > 0, resPID.Found, "byPID=%s", op.p.UniqID)
	}
}
