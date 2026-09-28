// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	ttyapi "github.com/wippyai/runtime/api/tty"
	securitysys "github.com/wippyai/runtime/system/security"
	ttysys "github.com/wippyai/runtime/system/tty"
)

type luaMountPolicy struct{}

func (luaMountPolicy) ID() registry.ID { return registry.ParseID("test:mount") }
func (luaMountPolicy) Evaluate(_ security.Actor, action, _ string, _ attrs.Bag) security.Result {
	if action == "tty.mount" || action == ttyapi.RightObserve {
		return security.Allow
	}
	return security.Deny
}

func TestLuaMountRequiresPermissionsAndChecksEveryAccess(t *testing.T) {
	service := ttysys.NewService()
	defer service.Close()
	root := ttyapi.WithService(ctxapi.NewRootContext(), service)
	owner, ownerFrame := ctxapi.OpenFrameContext(root)
	defer ownerFrame.Close()
	agent, agentFrame := ctxapi.OpenFrameContext(root)
	defer agentFrame.Close()
	require.NoError(t, runtime.SetFramePID(owner, pid.PID{Node: "n", Host: "h", UniqID: "owner"}))
	require.NoError(t, runtime.SetFramePID(agent, pid.PID{Node: "n", Host: "h", UniqID: "agent"}))
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)
	l.SetContext(owner)
	require.NoError(t, l.DoString(`
  own=assert(tty.viewport({width=80,height=24}))
  local ref,err=own:mount("{n@h|agent}",{observe=true})
  assert(ref==nil and err,"missing scope must deny observation grants")
 `))
	require.NoError(t, security.SetActor(owner, security.Actor{ID: "owner"}))
	require.NoError(t, security.SetScope(owner, securitysys.NewScope([]security.Policy{luaMountPolicy{}})))
	require.NoError(t, l.DoString(`
  local ref,err=own:mount("{n@h|agent}",{observe=true,input=true})
  assert(ref==nil and err,"observe permission must not grant input")
  mount_ref=assert(own:mount("{n@h|agent}",{observe=true}))
 `))
	l.SetContext(agent)
	require.NoError(t, l.DoString(`
  -- Even if a userdata reaches another process, caller identity is checked.
  local result,err=own:snapshot(); assert(result==nil and err)
  result,err=own:grant(); assert(result==nil and err)
  result,err=own:handle(); assert(result==nil and err)
  result,err=own:close(); assert(result==nil and err)
  observer=assert(tty.attach(mount_ref))
  assert(observer:snapshot().width==80)
  result,err=observer:send({type="key",key="x",key_type="runes",action="press"})
  assert(result==nil and err)
  result,err=observer:resize(90,30); assert(result==nil and err)
  result,err=observer:mount("{n@h|other}",{observe=true}); assert(result==nil and err)
 `))
	l.SetContext(owner)
	require.NoError(t, l.DoString(`assert(own:revoke(mount_ref))`))
	l.SetContext(agent)
	require.NoError(t, l.DoString(`
  local snapshot,err=observer:snapshot(); assert(snapshot==nil and err)
  assert(observer:close()); assert(observer:close())
 `))
}
