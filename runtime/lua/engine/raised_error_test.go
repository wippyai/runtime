// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"errors"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/process"
)

func TestProcessRaisedTypedError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		script string
		init   bool
	}{
		{"body", `return {main=function() error(errors.new({message="bad declaration", kind=errors.INVALID, retryable=false, details={field="target"}})) end}`, false},
		{"initialization", `error(errors.new({message="bad declaration", kind=errors.INVALID, retryable=false, details={field="target"}}))`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			factory := NewFactory(FactoryConfig{Script: tc.script, ScriptName: "raised.lua", ModuleBinders: []ModuleBinder{wrapBinder(func(l *lua.LState) { lua.OpenErrors(l) })}})
			p, err := factory()
			if err != nil {
				t.Fatal(err)
			}
			proc := p.(*Process)
			defer proc.Close()
			ctx, _ := ctxapi.OpenFrameContext(context.Background())
			err = proc.Init(ctx, "main", nil)
			if !tc.init {
				if err != nil {
					t.Fatal(err)
				}
				var output process.StepOutput
				err = proc.Step(nil, &output)
				if output.Status() != process.StepDone {
					t.Fatalf("status = %v", output.Status())
				}
			}
			var apiErr apierror.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %T %v", err, err)
			}
			if apiErr.Kind() != apierror.Invalid || apiErr.Retryable() != apierror.False || apiErr.Error() != "bad declaration" || apiErr.Details().GetString("field", "") != "target" {
				t.Fatalf("metadata = %s/%s/%q/%v", apiErr.Kind(), apiErr.Retryable(), apiErr.Error(), apiErr.Details())
			}
		})
	}
}

func TestProcessRaisedTypedErrorAfterYield(t *testing.T) {
	factory := NewFactory(FactoryConfig{
		Script:        `return {main=function() test_yield(1); error(errors.new({message="after yield", kind=errors.UNAVAILABLE, retryable=true})) end}`,
		ScriptName:    "raised.lua",
		ModuleBinders: []ModuleBinder{bindTestYield, wrapBinder(func(l *lua.LState) { lua.OpenErrors(l) })},
	})
	p, err := factory()
	if err != nil {
		t.Fatal(err)
	}
	proc := p.(*Process)
	defer proc.Close()
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "main", nil); err != nil {
		t.Fatal(err)
	}
	var output process.StepOutput
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Yields()) != 1 {
		t.Fatalf("yields = %v", output.Yields())
	}
	tag := output.Yields()[0].Tag
	output.Reset()
	err = proc.Step([]process.Event{{Type: process.EventYieldComplete, Tag: tag}}, &output)
	var apiErr apierror.Error
	if output.Status() != process.StepDone || !errors.As(err, &apiErr) || apiErr.Kind() != apierror.Unavailable || apiErr.Retryable() != apierror.True || apiErr.Error() != "after yield" {
		t.Fatalf("result = %v/%v", output.Status(), err)
	}
}

func TestProcessReturnedLuaErrorKeepsCause(t *testing.T) {
	factory := NewFactory(FactoryConfig{
		Script:        `return {main=function() local inner=errors.new({message="inner", kind=errors.UNAVAILABLE, retryable=true}); return nil, errors.wrap(inner, "context") end}`,
		ScriptName:    "returned.lua",
		ModuleBinders: []ModuleBinder{wrapBinder(func(l *lua.LState) { lua.OpenErrors(l) })},
	})
	p, err := factory()
	if err != nil {
		t.Fatal(err)
	}
	proc := p.(*Process)
	defer proc.Close()
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "main", nil); err != nil {
		t.Fatal(err)
	}
	var output process.StepOutput
	err = proc.Step(nil, &output)
	var apiErr apierror.Error
	if !errors.As(err, &apiErr) || apiErr.Error() != "context: inner" || apiErr.Kind() != apierror.Unavailable {
		t.Fatalf("returned error = %v", err)
	}
	if chain := apierror.BuildChain(err); len(chain.Errors) < 2 || chain.Errors[1].Kind != string(apierror.Unavailable) {
		t.Fatalf("returned chain = %+v", chain)
	}
}

func TestProcessSyncExecuteRaisedTypedError(t *testing.T) {
	source := lua.NewState()
	fn, err := source.LoadString(`error(errors.new({message="sync error", kind=errors.INVALID, retryable=false}))`)
	if err != nil {
		t.Fatal(err)
	}
	proto := fn.Proto
	source.Close()
	proc := mustNewProcess(t, WithProto(proto))
	proc.state = lua.NewState()
	lua.OpenErrors(proc.state)
	defer proc.Close()
	_, err = proc.SyncExecute(context.Background())
	var apiErr apierror.Error
	var vm *lua.ApiError
	if !errors.As(err, &apiErr) || apiErr.Kind() != apierror.Invalid || apiErr.Retryable() != apierror.False || apiErr.Error() != "sync error" || !errors.As(err, &vm) {
		t.Fatalf("sync error = %v", err)
	}
}
