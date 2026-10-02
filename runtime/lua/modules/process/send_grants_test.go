// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	secapi "github.com/wippyai/runtime/api/security"
	systemtopology "github.com/wippyai/runtime/system/topology"
)

var (
	grantsParent = pid.PID{Host: "h1", UniqID: "parent"}
	grantsOther  = pid.PID{Host: "h1", UniqID: "other"}
)

// restrictLua installs send grants (seeded with self and parent) on the
// test process frame, making it capability-restricted.
func restrictLua(t *testing.T, l *lua.LState, self pid.PID) *secapi.ProcessSendGrants {
	t.Helper()
	grants := secapi.NewProcessSendGrants(self, grantsParent)
	fc := ctxapi.FrameFromContext(l.Context())
	require.NoError(t, fc.SetMultiple(secapi.ProcessSendGrantsPair(grants)))
	return grants
}

func TestResolvePIDRestrictedProcessRefusesUngrantedPID(t *testing.T) {
	l, self := newLuaWithPID(t)
	restrictLua(t, l, self)

	_, err := resolvePID(l, grantsOther.String(), "process.send", self)
	require.ErrorContains(t, err, "not allowed to send")

	resolved, err := resolvePID(l, grantsParent.String(), "process.send", self)
	require.NoError(t, err)
	require.Equal(t, grantsParent.String(), resolved.PID.String())

	_, err = resolvePID(l, self.String(), "process.send", self)
	require.NoError(t, err, "a process may address itself")

	_, err = resolvePID(l, grantsOther.String(), "process.terminate", self)
	require.ErrorContains(t, err, "not allowed to terminate")
}

func TestResolvePIDUnrestrictedProcessUnchanged(t *testing.T) {
	l, self := newLuaWithPID(t)
	resolved, err := resolvePID(l, grantsOther.String(), "process.send", self)
	require.NoError(t, err)
	require.Equal(t, grantsOther.String(), resolved.PID.String())
}

func TestProcessSendSpoofedPIDFailsInLua(t *testing.T) {
	l, self := newLuaWithPID(t)
	restrictLua(t, l, self)
	require.NoError(t, l.DoString(`ok, err = process.send("`+grantsOther.String()+`", "topic", "x")`))
	require.Equal(t, lua.LNil, l.GetGlobal("ok"))
	require.Contains(t, l.GetGlobal("err").String(), "not allowed to send")
}

func TestMessageSenderIsGranted(t *testing.T) {
	l, self := newLuaWithPID(t)
	grants := restrictLua(t, l, self)

	l.SetGlobal("msg", WrapMessage(l, NewMessage(grantsOther, "topic", nil)))
	require.False(t, grants.Holds(grantsOther))
	require.NoError(t, l.DoString(`from = msg:from()`))
	require.Equal(t, grantsOther.String(), l.GetGlobal("from").String())
	require.True(t, grants.Holds(grantsOther))
}

func TestSpawnResultIsGranted(t *testing.T) {
	l, self := newLuaWithPID(t)
	grants := restrictLua(t, l, self)

	child := pid.PID{Host: "h1", UniqID: "child"}
	y := &SpawnYield{}
	ret := y.HandleResult(l, process.SpawnResult{PID: child}, nil)
	require.Equal(t, child.String(), ret[0].String())
	require.True(t, grants.Holds(child))

	failed := y.HandleResult(l, process.SpawnResult{PID: grantsOther, Error: context.Canceled}, nil)
	require.Equal(t, lua.LNil, failed[0])
	require.False(t, grants.Holds(grantsOther), "a failed spawn grants nothing")
}

func TestRegistryLookupIsGranted(t *testing.T) {
	reg := systemtopology.NewPIDRegistry()
	_, err := reg.Register("worker", grantsOther)
	require.NoError(t, err)
	l, self := newLuaWithPIDAndRegistry(t, reg)
	grants := restrictLua(t, l, self)

	require.NoError(t, l.DoString(`found = process.registry.lookup("worker")`))
	require.Equal(t, grantsOther.String(), l.GetGlobal("found").String())
	require.True(t, grants.Holds(grantsOther))
}
