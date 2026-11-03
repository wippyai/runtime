// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	securityapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	enginepayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	"github.com/wippyai/runtime/service/temporal/internal/securitykeys"
	"github.com/wippyai/runtime/service/temporal/propagator"
	syspayload "github.com/wippyai/runtime/system/payload"
	commonpb "go.temporal.io/api/common/v1"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.uber.org/zap"
)

type initializingEnvironment struct{ *recordedEnvironment }

func (*initializingEnvironment) RegisterCancelHandler(func()) {}
func (*initializingEnvironment) RegisterSignalHandler(func(string, *commonpb.Payloads, *commonpb.Header) error) {
}
func (*initializingEnvironment) RegisterQueryHandler(func(string, *commonpb.Payloads, *commonpb.Header) (*commonpb.Payloads, error)) {
}
func (*initializingEnvironment) RegisterUpdateHandler(func(string, string, *commonpb.Payloads, *commonpb.Header, bindings.UpdateCallbacks)) {
}
func (*initializingEnvironment) IsReplaying() bool { return false }

func luaMailboxDefinition(t *testing.T, source string, header *commonpb.Header, keys ...[]byte) (*Definition, *recordedEnvironment) {
	t.Helper()
	return luaMailboxDefinitionWithRun(t, source, header, "", keys...)
}

func luaMailboxDefinitionWithRun(t *testing.T, source string, header *commonpb.Header, continuedFrom string, keys ...[]byte) (*Definition, *recordedEnvironment) {
	t.Helper()
	_, env := newRouterDefinition(t, nil)
	env.info.ContinuedExecutionRunID = continuedFrom
	ctx := securityapi.SetStrictMode(ctxapi.NewRootContext(), false)
	transcoder := syspayload.NewTranscoder()
	enginepayload.RegisterAllBasicFormats(transcoder)
	payload.WithTranscoder(ctx, transcoder)
	manager, err := code.NewCodeManager(zap.NewNop(), nil, code.Config{Modules: []*luaapi.ModuleDef{processmod.Module}})
	require.NoError(t, err)
	id := registry.NewID("test", "mailbox_upgrade")
	require.NoError(t, manager.AddNode(ctx, code.Node{ID: id, Kind: luaapi.Workflow, Method: "main", Source: source}, []code.Import{{ID: registry.NewID("", "process"), Alias: "process"}}))
	create, err := engine.NewProcessFactory(manager).CreateFactory(id, engine.WithAllowedClasses(luaapi.ClassDeterministic, luaapi.ClassWorkflow))
	require.NoError(t, err)
	ctx = process.WithFactory(ctx, deliveryFactory{create: create})
	ctx = securitykeys.WithKeys(ctx, keys...)
	if len(keys) > 0 && header == nil {
		header, err = propagator.AddSecurityToHeader(env.dc, nil, &propagator.SecurityPayload{Audience: "self-workflow", Actor: &propagator.ActorPayload{ID: "user"}, Scope: true}, keys...)
		require.NoError(t, err)
	}
	d := &Definition{id: id, ctx: ctx, log: zap.NewNop()}
	d.Execute(&initializingEnvironment{env}, header, nil)
	require.Empty(t, env.completions)
	t.Cleanup(d.Close)
	return d, env
}

func TestContinueAsNewPreservesUnreadLuaMailbox(t *testing.T) {
	for _, secured := range []bool{false, true} {
		t.Run(fmt.Sprint(secured), func(t *testing.T) {
			var keys [][]byte
			if secured {
				keys = [][]byte{[]byte("0123456789abcdef0123456789abcdef")}
			}

			old, oldEnv := luaMailboxDefinition(t, `
 local process = require("process")
 return function()
  local inbox = process.listen("data", {message=true})
  local upgrade = process.listen("upgrade")
  upgrade:receive()
  process.upgrade(nil, "next-input")
 end`, nil, keys...)
			old.OnWorkflowTaskStarted(0)
			sender := pid.PID{Node: "test", Host: "sender", UniqID: "one"}
			header, err := propagator.CreateHeader(old.dc, map[string]any{propagator.SignalFromValueKey: sender.String()})
			require.NoError(t, err)
			var gateHeader *commonpb.Header
			if secured {
				header, err = propagator.AddRelaySignalToHeader(old.dc, header, propagator.RelaySignalTicket{Audience: "self-workflow", Operation: propagator.RelaySignalOperation, Signal: "data"}, keys...)
				require.NoError(t, err)
				gateHeader, err = propagator.AddRelaySignalToHeader(old.dc, nil, propagator.RelaySignalTicket{Audience: "self-workflow", Operation: propagator.RelaySignalOperation, Signal: "upgrade"}, keys...)
				require.NoError(t, err)
			}
			for _, value := range []string{"first", "second"} {
				input, err := old.dc.ToPayloads(payload.Payloads{payload.NewString(value)})
				require.NoError(t, err)
				require.NoError(t, old.handleSignal("data", input, header))
			}
			require.NoError(t, old.handleSignal("upgrade", nil, gateHeader))
			old.OnWorkflowTaskStarted(0)
			require.Len(t, oldEnv.completions, 1)
			var continuation *bindings.ContinueAsNewError
			require.True(t, errors.As(oldEnv.completions[0].err, &continuation))
			require.NotNil(t, continuation.Header, "continue-as-new must carry the unread mailbox")
			require.Empty(t, oldEnv.versions)
			next, nextEnv := luaMailboxDefinitionWithRun(t, `
 local process = require("process")
 return function()
  local inbox = process.listen("data", {message=true})
  local values = {}
  for i=1,3 do
   local msg = inbox:receive()
   values[i] = {value=msg:data(), from=tostring(msg:from()), topic=msg:topic()}
  end
  return values
			end`, continuation.Header, "self-run", keys...)
			wire, err := next.dc.ToPayloads(payload.Payloads{payload.NewString("third")})
			require.NoError(t, err)
			if secured {
				require.ErrorContains(t, next.handleSignal("data", wire, nil), "signed")
			}
			require.NoError(t, next.handleSignal("data", wire, header))
			next.OnWorkflowTaskStarted(0)
			require.Len(t, nextEnv.completions, 1, "carried messages must precede the new signal")
			require.NoError(t, nextEnv.completions[0].err)
			var got []struct{ Value, From, Topic string }
			require.NoError(t, next.dc.FromPayloads(nextEnv.completions[0].result, &got))
			require.Equal(t, []struct{ Value, From, Topic string }{{"first", sender.String(), "data"}, {"second", sender.String(), "data"}, {"third", sender.String(), "data"}}, got)
		})
	}
}

func TestContinueAsNewCarriesSignalsBeforeFirstStep(t *testing.T) {
	d, env := newRouterDefinition(t, nil)
	d.id = registry.NewID("test", "upgrade")
	d.signals = []incomingSignal{{Name: "data", Payloads: payload.Payloads{payload.NewString("unread")}}}
	d.output.SetUpgrade(&process.UpgradeRequest{Input: payload.Payloads{payload.NewString("next-input")}})
	d.executeContinueAsNew()
	var continuation *bindings.ContinueAsNewError
	require.True(t, errors.As(env.completions[0].err, &continuation))
	messages, err := propagator.ExtractMailboxFromHeader(d.dc, continuation.Header, "self-workflow", "first-run")
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "data", messages[0].Topic)
	var input string
	require.NoError(t, d.dc.FromPayloads(continuation.Input, &input))
	require.Equal(t, "next-input", input)
}
