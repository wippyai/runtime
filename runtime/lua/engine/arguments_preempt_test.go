// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"errors"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/bytecode"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
)

// Argument rejection may move from Init to Step,
// but it must still occur before the selected annotated handler executes.
func TestPreemptibleAnnotatedHandlerRejectsBeforeBody(t *testing.T) {
	for _, dumped := range []bool{false, true} {
		name := map[bool]string{false: "source", true: "bytecode"}[dumped]
		for _, invalid := range []bool{false, true} {
			caseName := map[bool]string{false: "wrong-type", true: "failed-conversion"}[invalid]
			t.Run(name+"/"+caseName, func(t *testing.T) {
				proto := compileArgumentEntry(t, `return {run=function(id: string?) _G.handled = true; return true end}`)
				if dumped {
					blob, err := bytecode.Dump(proto)
					if err != nil {
						t.Fatal(err)
					}
					proto, err = bytecode.Undump(blob)
					if err != nil {
						t.Fatal(err)
					}
				}
				p, err := NewFactory(FactoryConfig{Proto: proto, ValidateArguments: true})()
				if err != nil {
					t.Fatal(err)
				}
				defer p.Close()
				proc := p.(*Process)
				proc.EnablePreemption()
				ctx, fc := ctxapi.OpenFrameContext(context.Background())
				defer ctxapi.ReleaseFrameContext(fc)
				pl := payload.NewPayload(lua.LInteger(42), payload.Lua)
				if invalid {
					pl = payload.NewPayload("present", payload.Format("unsupported"))
				}
				err = proc.Init(ctx, "run", payload.Payloads{pl})
				if err == nil {
					var out process.StepOutput
					for steps := 0; steps < 100; steps++ {
						out.Reset()
						err = proc.Step(nil, &out)
						if err != nil || out.Status() == process.StepDone {
							break
						}
					}
				}
				var apiErr apierror.Error
				if !errors.As(err, &apiErr) || apiErr.Kind() != apierror.Invalid {
					t.Errorf("rejection = %v, want Invalid", err)
				}
				if proc.state.GetGlobal("handled") != lua.LNil {
					t.Error("invalid input reached the selected handler body")
				}
			})
		}
	}
}

func TestDeferredArgumentCheckSurvivesInitializerPreemption(t *testing.T) {
	proto := compileArgumentEntry(t, `return {run=function(id: string?) _G.handled=true; return true end}`)
	initializer, err := lua.CompileString(`local n=0; for i=1,1000 do n=n+i end; return n`, "initializer.lua")
	if err != nil {
		t.Fatal(err)
	}
	completed := 0
	p, err := NewFactory(FactoryConfig{
		Proto: proto, ValidateArguments: true,
		Budgets: luaapi.ExecutionBudgets{TickBudget: 10, TickBudgetSet: true},
		ModuleBinders: []ModuleBinder{wrapBinder(func(l *lua.LState) {
			DeferInitializer(l, Initializer{Fn: l.LoadProto(initializer), Done: func(lua.LValue) { completed++ }})
		})},
	})()
	if err != nil {
		t.Fatal(err)
	}
	proc := p.(*Process)
	defer proc.Close()
	proc.EnablePreemption()
	ctx, frame := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(frame)
	// Conversion is attempted once at Init, but cannot become optional nil after
	// the initializer's many preemptions and the subsequent handler selection.
	if err := proc.Init(ctx, "run", payload.Payloads{payload.NewPayload("value", payload.Format("unsupported"))}); err != nil {
		t.Fatal(err)
	}
	preemptions := 0
	var out process.StepOutput
	for i := 0; i < 10000; i++ {
		out.Reset()
		err = proc.Step(nil, &out)
		if err != nil || out.Status() == process.StepDone {
			break
		}
		if out.Status() == process.StepPreempted {
			preemptions++
		}
	}
	var apiErr apierror.Error
	if !errors.As(err, &apiErr) || apiErr.Kind() != apierror.Invalid {
		t.Fatalf("rejection=%v; want Invalid", err)
	}
	if preemptions == 0 || completed != 1 {
		t.Fatalf("initializer preemptions=%d completions=%d", preemptions, completed)
	}
	if proc.state.GetGlobal("handled") != lua.LNil {
		t.Fatal("invalid input entered the handler")
	}
	if proc.argumentError != nil {
		t.Fatal("completed rejection retained conversion error")
	}
	// A cached valid call is neither contaminated by the error nor reinitialized.
	ctx2, frame2 := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(frame2)
	_, err = executeArgumentEntry(ctx2, t, proc, "run", payload.Payloads{payload.NewPayload(lua.LString("ok"), payload.Lua)})
	if err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Fatal("cached call repeated initializer")
	}
}

func TestDeferredAndCachedArgumentArity(t *testing.T) {
	cases := []struct {
		name    string
		params  string
		valid   []lua.LValue
		invalid []lua.LValue
	}{
		{"required", "id: string", []lua.LValue{lua.LString("ok")}, nil},
		{"optional", "id: string?", nil, []lua.LValue{lua.LInteger(1)}},
		{"variadic", "...: string", []lua.LValue{lua.LString("a"), lua.LString("b")}, []lua.LValue{lua.LString("a"), lua.LInteger(1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proto := compileArgumentEntry(t, `return {run=function(`+tc.params+`) _G.handled=true; return true end}`)
			p, err := NewFactory(FactoryConfig{Proto: proto, ValidateArguments: true, Budgets: luaapi.ExecutionBudgets{TickBudget: 10, TickBudgetSet: true}})()
			if err != nil {
				t.Fatal(err)
			}
			proc := p.(*Process)
			defer proc.Close()
			proc.EnablePreemption()
			call := func(values []lua.LValue) error {
				ctx, frame := ctxapi.OpenFrameContext(context.Background())
				defer ctxapi.ReleaseFrameContext(frame)
				input := make(payload.Payloads, len(values))
				for i, v := range values {
					input[i] = payload.NewPayload(v, payload.Lua)
				}
				_, err := executeArgumentEntry(ctx, t, proc, "run", input)
				return err
			}
			for attempt := 0; attempt < 2; attempt++ {
				proc.state.SetGlobal("handled", lua.LNil)
				var apiErr apierror.Error
				if err := call(tc.invalid); !errors.As(err, &apiErr) || apiErr.Kind() != apierror.Invalid {
					t.Fatalf("attempt %d: rejection=%v", attempt, err)
				}
				if proc.state.GetGlobal("handled") != lua.LNil {
					t.Fatal("rejected argument entered handler")
				}
				if err := call(tc.valid); err != nil {
					t.Fatal(err)
				}
				if proc.state.GetGlobal("handled") != lua.LTrue {
					t.Fatal("valid argument did not enter handler")
				}
			}
		})
	}
}

func TestArgumentChecksFollowSelectedMethod(t *testing.T) {
	proto := compileArgumentEntry(t, `return {run=function(id: string) return true end, other=function(id: integer) _G.handled=true; return true end}`)
	p, err := NewFactory(FactoryConfig{Proto: proto, ValidateArguments: true})()
	if err != nil {
		t.Fatal(err)
	}
	proc := p.(*Process)
	defer proc.Close()
	for _, method := range []string{"run", "other", "other"} {
		ctx, frame := ctxapi.OpenFrameContext(context.Background())
		_, err := executeArgumentEntry(ctx, t, proc, method, payload.Payloads{payload.NewPayload(lua.LString("ok"), payload.Lua)})
		ctxapi.ReleaseFrameContext(frame)
		if method == "run" {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		var apiErr apierror.Error
		if !errors.As(err, &apiErr) || apiErr.Kind() != apierror.Invalid {
			t.Fatalf("selected method rejection=%v", err)
		}
		if proc.state.GetGlobal("handled") != lua.LNil {
			t.Fatal("another method's contract was used")
		}
	}
}
