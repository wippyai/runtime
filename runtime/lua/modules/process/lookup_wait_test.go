// SPDX-License-Identifier: MPL-2.0

package process

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/topology"
	systemtopology "github.com/wippyai/runtime/system/topology"
)

func TestRegistryLookup_TimeoutReturnsABoundNameWithoutWaiting(t *testing.T) {
	bound := pid.PID{Node: "node-B", Host: "app", UniqID: "sup"}
	l, _ := newLuaWithPIDAndRegistry(t, systemtopology.NewPIDRegistry())
	topology.WithEventualRegistry(l.Context(), &fakeEventualRegistry{entries: map[string]pid.PID{"service": bound}})
	require.NoError(t, l.DoString(fmt.Sprintf(`
        local owner, err = process.registry.lookup("service", process.registry.EVENTUAL, {timeout = "5s"})
        assert(owner == %q, tostring(err))
    `, bound.String())))
}

func TestRegistryLookup_TimeoutYieldsForAnUnboundName(t *testing.T) {
	l, _ := newLuaWithPIDAndRegistry(t, systemtopology.NewPIDRegistry())
	topology.WithEventualRegistry(l.Context(), &fakeEventualRegistry{})
	options := l.NewTable()
	options.RawSetString("timeout", lua.LString("250ms"))
	l.SetTop(0)
	l.Push(lua.LString("service"))
	l.Push(lua.LNumber(topology.Eventual))
	l.Push(options)

	require.Equal(t, -1, registryLookup(l))
	yield, ok := l.Get(-1).(*LookupWaitYield)
	require.True(t, ok, "lookup must yield a LookupWaitYield for an unbound name")
	require.Equal(t, "service", yield.Name)
	require.Equal(t, 250*time.Millisecond, yield.Timeout)
	require.Equal(t, process.LookupWait, yield.CmdID())
}

func TestRegistryLookup_TimeoutOptionsAreValidated(t *testing.T) {
	l, _ := newLuaWithPIDAndRegistry(t, systemtopology.NewPIDRegistry())
	topology.WithEventualRegistry(l.Context(), &fakeEventualRegistry{})
	for _, call := range []string{
		`process.registry.lookup("service", process.registry.LOCAL, {timeout = "5s"})`,
		`process.registry.lookup("service", process.registry.CONSISTENT, {timeout = "5s"})`,
		`process.registry.lookup("service", process.registry.EVENTUAL, {timeout = "soon"})`,
		`process.registry.lookup("service", process.registry.EVENTUAL, {timeout = "-1s"})`,
		`process.registry.lookup("service", process.registry.EVENTUAL, {timeout = 5})`,
		`process.registry.lookup("service", process.registry.EVENTUAL, "5s")`,
	} {
		require.NoError(t, l.DoString(`
            local owner, err = `+call+`
            assert(owner == nil)
            assert(err ~= nil and err:kind() == "Invalid", tostring(err))
        `), call)
	}
}

func TestRegistryLookup_WithoutTimeoutStaysASnapshot(t *testing.T) {
	l, _ := newLuaWithPIDAndRegistry(t, systemtopology.NewPIDRegistry())
	topology.WithEventualRegistry(l.Context(), &fakeEventualRegistry{})
	require.NoError(t, l.DoString(`
        local owner, err = process.registry.lookup("service", process.registry.EVENTUAL, {})
        assert(owner == nil)
        assert(err ~= nil and err:kind() == "NotFound", tostring(err))
    `))
}

func TestLookupWaitYield_HandleResult(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bound := pid.PID{Node: "node-B", Host: "app", UniqID: "sup"}
	y := AcquireLookupWaitYield()
	defer y.Release()
	y.Name, y.Timeout = "service", 2*time.Second

	found := y.HandleResult(l, process.LookupWaitResult{PID: bound, Found: true}, nil)
	require.Equal(t, lua.LString(bound.String()), found[0])
	require.Equal(t, lua.LNil, found[1])

	missing := y.HandleResult(l, process.LookupWaitResult{}, nil)
	require.Equal(t, lua.LNil, missing[0])
	notFound, ok := missing[1].(*lua.Error)
	require.True(t, ok)
	require.Equal(t, lua.NotFound, notFound.Kind())
	require.Contains(t, notFound.Error(), "within 2s")

	failed := y.HandleResult(l, nil, errors.New("registry stopped"))
	require.Equal(t, lua.LNil, failed[0])
	failure, ok := failed[1].(*lua.Error)
	require.True(t, ok)
	require.Equal(t, lua.Internal, failure.Kind())
}
