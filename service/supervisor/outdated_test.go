// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/supervisor"
	topologyapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/runtime/lua/engine"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	"github.com/wippyai/runtime/service/host"
	"github.com/wippyai/runtime/system/eventbus"
	systempayload "github.com/wippyai/runtime/system/payload"
	sysprocess "github.com/wippyai/runtime/system/process"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
	syssupervisor "github.com/wippyai/runtime/system/supervisor"
	systopology "github.com/wippyai/runtime/system/topology"
	"go.uber.org/zap"
)

type serviceIncarnation struct {
	at      time.Time
	pid     pid.PID
	version string
}

func TestSupervisedLuaOutdatedTransition(t *testing.T) {
	for _, mode := range []string{"exit", "native", "ordinary", "ignored", "nonupgradable", "unchanged", "dropped"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			bus := eventbus.NewBus()
			defer bus.Stop()
			factory := sysprocess.NewFactoryRegistry(bus, zap.NewNop())
			require.NoError(t, factory.Start(ctx))
			defer factory.Stop()
			responses := make(chan event.Event, 4)
			sub, err := eventbus.NewSubscriber(ctx, bus, processapi.System, "factory.(accept|reject)", func(e event.Event) {
				responses <- e
			})
			require.NoError(t, err)
			defer sub.Close()

			node := sysrelay.NewNode("test-node")
			router := sysrelay.NewRouter(node, nil)
			topo := systopology.NewTopology(router, node.ID())
			lifecycle := sysprocess.NewLifecycleRegistry()
			lifecycle.Register("topology", systopology.NewLifecycle(topo, nil, zap.NewNop()))
			sched := actor.NewScheduler(scheduler.NewRegistry(), actor.WithWorkers(1), actor.WithLifecycle(lifecycle))
			pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), node.ID())
			hostID := registry.NewID("test", "host")
			h := host.NewHost(hostID, nil, sched, factory, pidGen, zap.NewNop())
			lifecycle.Register("host", h)
			appCtx := setupTestContext(node, topo, sysprocess.NewManager(node, zap.NewNop()))
			transcoder := systempayload.NewTranscoder()
			luapayload.Register(transcoder)
			payload.WithTranscoder(appCtx, transcoder)
			processapi.WithFactory(appCtx, factory)
			require.NoError(t, node.RegisterHost(hostID.String(), h))
			require.NoError(t, node.RegisterHost(topologyapi.ControlHost, sysrelay.NewMailbox(ctx)))
			_, err = h.Start(appCtx)
			require.NoError(t, err)
			defer func() {
				stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				require.NoError(t, h.Stop(stopCtx))
			}()

			entered := make(chan serviceIncarnation, 8)
			release := make(chan struct{})
			defer func() {
				if release != nil {
					close(release)
				}
			}()
			var creates atomic.Int32
			procID := registry.NewID("test", "proc")
			register := func(script string, blocked <-chan struct{}) {
				proto, compileErr := lua.CompileString(script, "service.lua")
				require.NoError(t, compileErr)
				bus.Send(ctx, event.Event{System: processapi.System, Kind: processapi.FactoryRegister, Path: procID.String(),
					Data: &processapi.FactoryEntry{Meta: processapi.Meta{Method: "main"}, Factory: func() (processapi.Process, error) {
						creates.Add(1)
						return engine.NewProcess(engine.WithProto(proto), engine.WithModuleBinder(func(l *lua.LState) error {
							engine.LoadModuleDef(l, engine.ChannelModule)
							mod, _ := processmod.Module.Build()
							l.SetGlobal("process", mod)
							l.SetGlobal("report", l.NewFunction(func(l *lua.LState) int {
								id, _ := runtime.GetFramePID(l.Context())
								entered <- serviceIncarnation{pid: id, version: l.CheckString(1), at: time.Now()}
								if blocked != nil {
									<-blocked
								}
								return 0
							}))
							return nil
						}))
					}}})
				select {
				case response := <-responses:
					require.Equal(t, processapi.FactoryAccept, response.Kind)
				case <-time.After(5 * time.Second):
					t.Fatal("factory replacement is not acknowledged")
				}
			}
			v1 := `local function main()
				process.set_options({upgradable = true})
				local events = process.events()
				report("v1")
				while true do
					local event = events:receive()
					if event.kind == process.event.OUTDATED then ACTION end
					if event.kind == process.event.CANCEL then return end
				end
			end
			return {main = main}`
			action := "return"
			if mode == "native" {
				action = "process.upgrade()"
			}
			if mode == "ignored" {
				action = "report(\"ignored\")"
			}
			if mode == "nonupgradable" || mode == "unchanged" {
				v1 = strings.ReplaceAll(v1, "upgradable = true", "upgradable = false")
			}
			if mode == "dropped" {
				v1 = strings.ReplaceAll(v1, "local events = process.events()", "local events = process.events(); events:close(); local inbox = process.inbox()")
			}
			if mode == "dropped" {
				v1 = strings.ReplaceAll(v1, `report("v1")`, `report("v1"); inbox:receive(); events = process.events()`)
			}
			if mode == "nonupgradable" || mode == "dropped" {
				v1 = strings.ReplaceAll(v1, "if event.kind == process.event.CANCEL then return end", `if event.kind == process.event.CANCEL then report("stopped"); return end`)
			}
			if mode == "ordinary" {
				v1 = `return {main = function() report("v1"); return end}`
			} else {
				v1 = strings.ReplaceAll(v1, "ACTION", action)
			}
			register(v1, release)
			svc := NewService(registry.NewID("test", "svc"), newTestService().config, pidGen)
			svc.config.HostID = hostID.String()
			svc.config.Process = procID
			const delay = 100 * time.Millisecond
			config := supervisor.LifecycleConfig{StartTimeout: time.Second, StopTimeout: time.Second,
				StableThreshold: time.Minute, RetryPolicy: supervisor.RetryPolicy{InitialDelay: delay, MaxDelay: delay, MaxAttempts: 1}}
			controllerCtx, controllerCancel := context.WithCancel(appCtx)
			defer controllerCancel()
			controller := syssupervisor.NewController(controllerCtx, svc, config, nil)
			require.NoError(t, controller.Start())
			first := awaitIncarnation(t, entered)
			require.Equal(t, "v1", first.version)
			register(`return {main = function()
				process.set_options({upgradable = true})
				local inbox = process.inbox()
 local events = process.events()
 report("v2")
 while true do
 local selected = channel.select({inbox:case_receive(), events:case_receive()})
 if selected.channel == inbox or selected.value.kind == process.event.CANCEL then return end
				end
			end}`, nil)
			transitionAt := time.Now()
			if mode == "unchanged" {
				sched.SendOutdated(map[registry.ID]bool{registry.NewID("test", "other"): true})
			}
			if mode != "ordinary" && mode != "unchanged" {
				for range 10 {
					sched.SendOutdated(map[registry.ID]bool{procID: true})
				}
			}
			close(release)
			release = nil
			if mode == "dropped" {
				require.NoError(t, node.Send(relay.NewPackage(pid.PID{}, first.pid, topologyapi.TopicInbox)))
			}
			switch mode {
			case "unchanged":
				require.Equal(t, supervisor.StatusRunning, controller.State().Status)
			case "ordinary":
				require.Eventually(t, func() bool { return controller.State().Status == supervisor.StatusExited }, 5*time.Second, time.Millisecond)
			case "ignored":
				ignored := awaitIncarnation(t, entered)
				require.Equal(t, "ignored", ignored.version)
				require.Equal(t, first.pid, ignored.pid)
			default:
				if mode == "nonupgradable" || mode == "dropped" {
					stopped := awaitIncarnation(t, entered)
					require.Equal(t, "stopped", stopped.version)
					require.Equal(t, first.pid, stopped.pid)
				}
				second := awaitIncarnation(t, entered)
				require.Equal(t, "v2", second.version)
				if mode == "native" {
					require.Equal(t, first.pid, second.pid)
				} else {
					require.NotEqual(t, first.pid, second.pid)
					require.GreaterOrEqual(t, second.at.Sub(transitionAt), delay)
				}
				require.NoError(t, node.Send(relay.NewPackage(pid.PID{}, second.pid, "finish")))
				require.Eventually(t, func() bool { return controller.State().Status == supervisor.StatusExited }, time.Second, time.Millisecond)
				require.NoError(t, controller.Stop())
			}
			select {
			case extra := <-entered:
				t.Fatalf("unexpected incarnation: %+v", extra)
			case <-time.After(3 * delay):
			}
			want := int32(2)
			if mode == "ordinary" || mode == "ignored" || mode == "unchanged" {
				want = 1
			}
			require.Equal(t, want, creates.Load(), "exactly one activation per code transition")
		})
	}
}

func awaitIncarnation(t *testing.T, entered <-chan serviceIncarnation) serviceIncarnation {
	t.Helper()
	select {
	case incarnation := <-entered:
		return incarnation
	case <-time.After(5 * time.Second):
		t.Fatal("service code does not activate")
	}
	return serviceIncarnation{}
}
