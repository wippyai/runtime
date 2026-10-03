// SPDX-License-Identifier: MPL-2.0

package engine

import (
	lua "github.com/wippyai/go-lua"
)

const initializersRegistryKey = "wippy.initializers"

// Initializer is Lua code that must run before a process entry point, such as
// a library chunk whose result later chunks import. A ModuleBinder registers
// it with DeferInitializer instead of calling the function itself, so the code
// runs as a Lua frame of the process's main task: scheduled, preemptible and
// limited by the tick budget like the rest of the actor.
type Initializer struct {
	// Fn is the function to run, created on the state the binder received. It
	// runs without arguments.
	Fn *lua.LFunction
	// Done receives the first value Fn returned, or nil. It runs on the
	// process's step goroutine, after Fn returns and before the next
	// initializer starts.
	Done func(result lua.LValue)
}

// DeferInitializer registers init to run on l's process before its entry
// point. Initializers run in registration order, once per state: a later
// execution of the same process finds them completed. An initializer that
// fails is retried by the next execution.
//
// Call it from a ModuleBinder. Registration order must follow dependency
// order, so a library's imports are initialized before the library.
func DeferInitializer(l *lua.LState, init Initializer) {
	list := initializersOf(l)
	if list == nil {
		list = &initializerList{}
		l.G.Registry.RawSetString(initializersRegistryKey, list)
	}
	list.items = append(list.items, init)
}

// initializerList is the registry-held sequence of a state's initializers.
// It implements lua.LValue so the state's registry can hold it.
type initializerList struct {
	items []Initializer
	// onComplete callbacks run once, when the last initializer completes.
	onComplete []func()
	// done counts completed initializers; started reports that advance has
	// handed out items[done] in the current run.
	done    int
	started bool
}

func (*initializerList) String() string       { return "<initializers>" }
func (*initializerList) Type() lua.LValueType { return lua.LTUserData }

// OnInitialized registers fn to run when l's deferred initializers have all
// completed, before the entry chunk runs. It does not run if none are
// registered with DeferInitializer.
func OnInitialized(l *lua.LState, fn func()) {
	list := initializersOf(l)
	if list == nil {
		list = &initializerList{}
		l.G.Registry.RawSetString(initializersRegistryKey, list)
	}
	list.onComplete = append(list.onComplete, fn)
}

func initializersOf(l *lua.LState) *initializerList {
	list, _ := l.G.Registry.RawGetString(initializersRegistryKey).(*initializerList)
	return list
}

func (list *initializerList) pending() bool {
	return list != nil && list.done < len(list.items)
}

// begin restarts the run at the first incomplete initializer.
func (list *initializerList) begin() {
	list.started = false
}

// advance is the Go side of the bootstrap loop. It takes the previous
// initializer's result (ignored on the first call) and returns the next
// function to run, or nothing when all have run.
func (list *initializerList) advance(l *lua.LState) int {
	if list.started {
		list.complete(l.Get(1))
	}
	list.started = list.done < len(list.items)
	if !list.started {
		return 0
	}
	l.Push(list.items[list.done].Fn)
	return 1
}

// runSync runs the incomplete initializers to completion on l's main thread,
// in order. It is the synchronous counterpart of the bootstrap loop for
// callers that execute without the scheduler; the code it runs is not
// preemptible.
func (list *initializerList) runSync(l *lua.LState) error {
	if !list.pending() {
		return nil
	}
	list.begin()
	for list.done < len(list.items) {
		item := list.items[list.done]
		if err := l.CallByParam(lua.P{Fn: item.Fn, NRet: 1, Protect: true}); err != nil {
			return err
		}
		result := l.Get(-1)
		l.Pop(1)
		list.complete(result)
	}
	return nil
}

// complete records the result of the current initializer and drops it, so the
// state keeps neither the function nor its callback after the run. The last
// completion runs the one-shot callbacks and drops them as well.
func (list *initializerList) complete(result lua.LValue) {
	item := list.items[list.done]
	list.items[list.done] = Initializer{}
	list.done++
	if item.Done != nil {
		item.Done(result)
	}
	if list.done < len(list.items) {
		return
	}
	callbacks := list.onComplete
	list.onComplete = nil
	for _, fn := range callbacks {
		fn()
	}
}
