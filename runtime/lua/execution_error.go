// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"errors"
	"reflect"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

type luaErrorView struct {
	source    *lua.Error
	envelope  *lua.ApiError
	cause     error
	message   string
	text      string
	kind      apierror.Kind
	details   attrs.Bag
	stack     []string
	retryable apierror.Ternary
}

type wrappedExecutionError struct {
	cause     error
	message   string
	kind      apierror.Kind
	details   attrs.Bag
	stack     []string
	retryable apierror.Ternary
}

func (e *wrappedExecutionError) Error() string               { return e.message }
func (e *wrappedExecutionError) Msg() string                 { return e.message }
func (e *wrappedExecutionError) Kind() apierror.Kind         { return e.kind }
func (e *wrappedExecutionError) Retryable() apierror.Ternary { return e.retryable }
func (e *wrappedExecutionError) Details() attrs.Attributes {
	if e.details == nil {
		return nil
	}
	return attrs.NewBagFrom(copyDetails(e.details))
}
func (e *wrappedExecutionError) StackFrames() []string { return append([]string(nil), e.stack...) }
func (e *wrappedExecutionError) Unwrap() error         { return e.cause }

func (e *luaErrorView) Error() string               { return e.text }
func (e *luaErrorView) Msg() string                 { return e.message }
func (e *luaErrorView) Kind() apierror.Kind         { return e.kind }
func (e *luaErrorView) Retryable() apierror.Ternary { return e.retryable }
func (e *luaErrorView) Details() attrs.Attributes {
	if e.details == nil {
		return nil
	}
	return attrs.NewBagFrom(copyDetails(e.details))
}
func (e *luaErrorView) StackFrames() []string { return append([]string(nil), e.stack...) }
func (e *luaErrorView) Unwrap() error         { return e.cause }
func (e *luaErrorView) Is(target error) bool  { return target == e.source }
func (e *luaErrorView) As(target any) bool {
	if e.envelope == nil {
		return false
	}
	if ptr, ok := target.(**lua.ApiError); ok {
		*ptr = e.envelope
		return true
	}
	return false
}

// ConvertExecutionError converts a VM execution failure before its Lua thread is released.
func ConvertExecutionError(state *lua.LState, err error) apierror.Error {
	if err == nil {
		return nil
	}
	var node any = err
	if apiErr, ok := node.(apierror.Error); ok {
		return apiErr
	}
	if envelope, ok := node.(*lua.ApiError); ok {
		if envelope.Type == lua.ApiErrorRun {
			if source, ok := lua.AsError(envelope.Object); ok {
				return newLuaErrorView(state, source, envelope)
			}
		}
		return executionFault(err)
	}
	if source, ok := node.(*lua.Error); ok {
		return newLuaErrorView(state, source, nil)
	}
	var categorized apierror.Error
	var rich apierror.Rich
	if errors.As(err, &categorized) || errors.As(err, &rich) {
		root := apierror.BuildChain(err).Root()
		view := &wrappedExecutionError{cause: err, message: err.Error(), kind: apierror.Kind(root.Kind), retryable: apierror.Unspecified, stack: append([]string(nil), root.Stack...)}
		if root.Retryable != nil {
			if *root.Retryable {
				view.retryable = apierror.True
			} else {
				view.retryable = apierror.False
			}
		}
		if root.Details != nil {
			view.details = attrs.NewBagFrom(copyDetails(root.Details))
		}
		return view
	}
	return executionFault(err)
}

func executionFault(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to execute script").WithCause(err).WithRetryable(apierror.False)
}

func newLuaErrorView(state *lua.LState, source *lua.Error, envelope *lua.ApiError) *luaErrorView {
	kind := apierror.Kind(source.Kind())
	if kind == "" {
		kind = apierror.Internal
	}
	retryable := apierror.Unspecified
	switch source.Retryable() {
	case lua.TernaryTrue:
		retryable = apierror.True
	case lua.TernaryFalse:
		retryable = apierror.False
	}
	var details attrs.Bag
	if source.Details() != nil {
		details = attrs.NewBagFrom(copyDetails(source.Details()))
	}
	message := source.Message
	if source.Context != "" {
		message = source.Context
	}
	view := &luaErrorView{
		source: source, envelope: envelope, message: message, text: source.Error(),
		kind: kind, retryable: retryable, details: details,
	}
	if source.LuaStack != nil {
		for _, frame := range source.LuaStack.Frames {
			view.stack = append(view.stack, frame.String())
		}
	}
	if len(view.stack) == 0 && state != nil {
		for level := 0; ; level++ {
			frame, ok := state.GetStack(level)
			if !ok {
				break
			}
			if _, infoErr := state.GetInfo("nSluf", frame, nil); infoErr != nil {
				break
			}
			view.stack = append(view.stack, lua.StackFrame{Source: frame.Source, CurrentLine: frame.CurrentLine, Name: frame.Name}.String())
		}
	}
	if envelope != nil && len(view.stack) == 0 && envelope.StackTrace != "" {
		view.stack = append(view.stack, envelope.StackTrace)
	}
	if source.Err != nil {
		var rawCause any = source.Err
		if child, ok := rawCause.(*lua.Error); ok {
			view.cause = newLuaErrorView(nil, child, nil)
		} else {
			view.cause = source.Err
		}
	}
	return view
}

func copyDetails(details map[string]any) map[string]any {
	copy := make(map[string]any, len(details))
	for key, value := range details {
		copy[key] = copyDetailValue(value)
	}
	return copy
}

func copyDetailValue(value any) any {
	if value == nil {
		return nil
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Map:
		out := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			original := iter.Value()
			copied := reflect.ValueOf(copyDetailValue(original.Interface()))
			if !copied.IsValid() {
				copied = reflect.Zero(original.Type())
			}
			out.SetMapIndex(iter.Key(), copied)
		}
		return out.Interface()
	case reflect.Slice:
		out := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		for i := 0; i < rv.Len(); i++ {
			original := rv.Index(i)
			copied := reflect.ValueOf(copyDetailValue(original.Interface()))
			if !copied.IsValid() {
				copied = reflect.Zero(original.Type())
			}
			out.Index(i).Set(copied)
		}
		return out.Interface()
	default:
		return value
	}
}
