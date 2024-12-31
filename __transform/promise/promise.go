package promise

import (
	"context"
	"fmt"
	transcoder "github.com/ponyruntime/pony/pkg/payload/lua"
	"sync"

	"github.com/ponyruntime/go-lua"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
)

// Promise represents a Lua userdata object wrapping an async operation
type Promise struct {
	resultChan chan *runtime.Result
	ctx        context.Context
	mtx        sync.RWMutex
	state      string // "pending", "fulfilled", or "rejected"
	result     *runtime.Result
}

// registerPromise promise methods
var promiseMethods = map[string]lua.LGFunction{
	"then":  promiseThen,
	"catch": promiseCatch,
	"state": promiseState,
}

// NewPromise creates a new Promise from a result channel
func NewPromise(ctx context.Context, resultChan chan *runtime.Result) *Promise {
	p := &Promise{
		resultChan: resultChan,
		ctx:        ctx,
		state:      "pending",
	}

	// Start listening for result
	go p.await()

	return p
}

func (p *Promise) await() {
	select {
	case result := <-p.resultChan:
		p.mtx.Lock()
		p.result = result
		if result.Error != nil {
			p.state = "rejected"
		} else {
			p.state = "fulfilled"
		}
		p.mtx.Unlock()
	case <-p.ctx.Done():
		p.mtx.Lock()
		p.state = "rejected"
		p.result = &runtime.Result{Error: p.ctx.Err()}
		p.mtx.Unlock()
	}
}

// Promise methods implementations
func promiseThen(l *lua.LState) int {
	// Get promise from userdata
	ud := l.CheckUserData(1)
	p, ok := ud.Value.(*Promise)
	if !ok {
		l.ArgError(1, "promise expected")
		return 0
	}

	callback := l.CheckFunction(2)

	// Create new promise for chaining
	newResultChan := make(chan *runtime.Result, 1)
	newPromise := NewPromise(p.ctx, newResultChan)

	// Create userdata for new promise
	newUd := l.NewUserData()
	newUd.Value = newPromise
	l.SetMetatable(newUd, l.GetTypeMetatable("Promise"))

	// Handle callback based on promise state
	p.mtx.RLock()
	state := p.state
	result := p.result
	p.mtx.RUnlock()

	switch state {
	case "fulfilled":
		if result != nil && result.Payload != nil {
			l.Push(callback)
			l.Push(transcoder.GoToLua(l, result.Payload.Data()))
			err := l.PCall(1, 1, nil)
			if err != nil {
				newResultChan <- &runtime.Result{Error: err}
			} else {
				// Handle callback result
				ret := l.Get(-1)
				l.Pop(1)

				// If callback returns a promise, chain it
				if ud, ok := ret.(*lua.LUserData); ok {
					if p, ok := ud.Value.(*Promise); ok {
						go func() {
							select {
							case r := <-p.resultChan:
								newResultChan <- r
							case <-p.ctx.Done():
								newResultChan <- &runtime.Result{Error: p.ctx.Err()}
							}
						}()
						l.Push(newUd)
						return 1
					}
				}

				// Otherwise wrap the result in a promise
				newResultChan <- &runtime.Result{
					Payload: payload.NewPayload(ret, payload.Lua),
				}
			}
		} else {
			newResultChan <- &runtime.Result{Payload: nil}
		}
	case "rejected":
		if result != nil {
			newResultChan <- result
		}
	case "pending":
		// Wait for resolution
		go func() {
			p.mtx.RLock()
			resultChan := p.resultChan
			p.mtx.RUnlock()

			select {
			case r := <-resultChan:
				if r.Error != nil {
					newResultChan <- r
				} else {
					l.Push(callback)
					if r.Payload != nil {
						l.Push(transcoder.GoToLua(l, r.Payload.Data()))
					} else {
						l.Push(lua.LNil)
					}
					if err := l.PCall(1, 1, nil); err != nil {
						newResultChan <- &runtime.Result{Error: err}
					} else {
						ret := l.Get(-1)
						l.Pop(1)
						newResultChan <- &runtime.Result{
							Payload: payload.NewPayload(ret, payload.Lua),
						}
					}
				}
			case <-p.ctx.Done():
				newResultChan <- &runtime.Result{Error: p.ctx.Err()}
			}
		}()
	}

	l.Push(newUd)
	return 1
}

func promiseCatch(l *lua.LState) int {
	ud := l.CheckUserData(1)
	p, ok := ud.Value.(*Promise)
	if !ok {
		l.ArgError(1, "promise expected")
		return 0
	}

	callback := l.CheckFunction(2)

	// Create new promise for chaining
	newResultChan := make(chan *runtime.Result, 1)
	newPromise := NewPromise(p.ctx, newResultChan)

	newUd := l.NewUserData()
	newUd.Value = newPromise
	l.SetMetatable(newUd, l.GetTypeMetatable("Promise"))

	p.mtx.RLock()
	state := p.state
	result := p.result
	p.mtx.RUnlock()

	switch state {
	case "rejected":
		if result != nil && result.Error != nil {
			l.Push(callback)
			l.Push(lua.LString(result.Error.Error()))
			if err := l.PCall(1, 1, nil); err != nil {
				newResultChan <- &runtime.Result{Error: err}
			} else {
				ret := l.Get(-1)
				l.Pop(1)
				newResultChan <- &runtime.Result{
					Payload: payload.NewPayload(ret, payload.Lua),
				}
			}
		}
	case "fulfilled":
		if result != nil {
			newResultChan <- result
		}
	case "pending":
		go func() {
			p.mtx.RLock()
			resultChan := p.resultChan
			p.mtx.RUnlock()

			select {
			case r := <-resultChan:
				if r.Error == nil {
					newResultChan <- r
				} else {
					l.Push(callback)
					l.Push(lua.LString(r.Error.Error()))
					if err := l.PCall(1, 1, nil); err != nil {
						newResultChan <- &runtime.Result{Error: err}
					} else {
						ret := l.Get(-1)
						l.Pop(1)
						newResultChan <- &runtime.Result{
							Payload: payload.NewPayload(ret, payload.Lua),
						}
					}
				}
			case <-p.ctx.Done():
				l.Push(callback)
				l.Push(lua.LString(p.ctx.Err().Error()))
				if err := l.PCall(1, 1, nil); err != nil {
					newResultChan <- &runtime.Result{Error: err}
				} else {
					ret := l.Get(-1)
					l.Pop(1)
					newResultChan <- &runtime.Result{
						Payload: payload.NewPayload(ret, payload.Lua),
					}
				}
			}
		}()
	}

	l.Push(newUd)
	return 1
}

func promiseState(l *lua.LState) int {
	ud := l.CheckUserData(1)
	p, ok := ud.Value.(*Promise)
	if !ok {
		l.ArgError(1, "promise expected")
		return 0
	}

	p.mtx.RLock()
	state := p.state
	p.mtx.RUnlock()

	l.Push(lua.LString(state))
	return 1
}

// registerPromise the Promise type
func registerPromise(l *lua.LState) {
	mt := l.NewTypeMetatable("Promise")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), promiseMethods))
}

// Static Promise methods
var promiseStaticMethods = map[string]lua.LGFunction{
	"all":        promiseAll,
	"any":        promiseAny,
	"allSettled": promiseAllSettled,
	"race":       promiseRace,
}

func promiseAll(l *lua.LState) int {
	promises := l.CheckTable(1)
	if promises.Len() == 0 {
		// Return resolved promise with empty array
		return createResolvedPromise(l, l.NewTable())
	}

	ctx := getContextFromFirstPromise(l, promises)
	if ctx == nil {
		l.ArgError(1, "no valid promises in array")
		return 0
	}

	resultChan := make(chan *runtime.Result, 1)
	var wg sync.WaitGroup
	results := l.NewTable()
	errorChan := make(chan error, 1)

	promises.ForEach(func(i, v lua.LValue) {
		if ud, ok := v.(*lua.LUserData); ok {
			if p, ok := ud.Value.(*Promise); ok {
				wg.Add(1)
				go func(index lua.LNumber) {
					defer wg.Done()
					select {
					case r := <-p.resultChan:
						if r.Error != nil {
							select {
							case errorChan <- r.Error:
							default:
							}
							return
						}
						if r.Payload != nil {
							results.RawSetInt(int(index), transcoder.GoToLua(l, r.Payload.Data()))
						} else {
							results.RawSetInt(int(index), lua.LNil)
						}
					case <-ctx.Done():
						select {
						case errorChan <- ctx.Err():
						default:
						}
					}
				}(lua.LNumber(i))
			}
		}
	})

	// Wait for all promises and send final result
	go func() {
		wg.Wait()
		select {
		case err := <-errorChan:
			resultChan <- &runtime.Result{Error: err}
		default:
			resultChan <- &runtime.Result{
				Payload: payload.NewPayload(results, payload.Lua),
			}
		}
		close(resultChan)
	}()

	// Create and return new promise
	return createPromiseFromChan(l, ctx, resultChan)
}

func promiseAny(l *lua.LState) int {
	promises := l.CheckTable(1)
	if promises.Len() == 0 {
		return createRejectedPromise(l, fmt.Errorf("no promises provided"))
	}

	ctx := getContextFromFirstPromise(l, promises)
	if ctx == nil {
		l.ArgError(1, "no valid promises in array")
		return 0
	}

	resultChan := make(chan *runtime.Result, 1)
	var wg sync.WaitGroup
	errors := make([]error, promises.Len())
	allRejected := make(chan bool, 1)

	promises.ForEach(func(i, v lua.LValue) {
		if ud, ok := v.(*lua.LUserData); ok {
			if p, ok := ud.Value.(*Promise); ok {
				wg.Add(1)
				go func(index int) {
					defer wg.Done()
					select {
					case r := <-p.resultChan:
						if r.Error == nil {
							// Found a successful promise
							select {
							case resultChan <- r:
							default:
							}
						} else {
							errors[index] = r.Error
						}
					case <-ctx.Done():
						errors[index] = ctx.Err()
					}
				}(int(i.(lua.LNumber)) - 1)
			}
		}
	})

	// Check if all promises rejected
	go func() {
		wg.Wait()
		if len(resultChan) == 0 {
			allRejected <- true
		}
		close(allRejected)
	}()

	go func() {
		select {
		case <-allRejected:
			// All promises rejected
			resultChan <- &runtime.Result{
				Error: fmt.Errorf("all promises rejected: %v", errors),
			}
		case <-ctx.Done():
			resultChan <- &runtime.Result{Error: ctx.Err()}
		}
		close(resultChan)
	}()

	return createPromiseFromChan(l, ctx, resultChan)
}

func promiseRace(l *lua.LState) int {
	promises := l.CheckTable(1)
	if promises.Len() == 0 {
		return createRejectedPromise(l, fmt.Errorf("no promises provided"))
	}

	ctx := getContextFromFirstPromise(l, promises)
	if ctx == nil {
		l.ArgError(1, "no valid promises in array")
		return 0
	}

	resultChan := make(chan *runtime.Result, 1)

	promises.ForEach(func(_, v lua.LValue) {
		if ud, ok := v.(*lua.LUserData); ok {
			if p, ok := ud.Value.(*Promise); ok {
				go func() {
					select {
					case r := <-p.resultChan:
						select {
						case resultChan <- r:
						default:
						}
					case <-ctx.Done():
						select {
						case resultChan <- &runtime.Result{Error: ctx.Err()}:
						default:
						}
					}
				}()
			}
		}
	})

	return createPromiseFromChan(l, ctx, resultChan)
}

func promiseAllSettled(l *lua.LState) int {
	promises := l.CheckTable(1)
	if promises.Len() == 0 {
		return createResolvedPromise(l, l.NewTable())
	}

	ctx := getContextFromFirstPromise(l, promises)
	if ctx == nil {
		l.ArgError(1, "no valid promises in array")
		return 0
	}

	resultChan := make(chan *runtime.Result, 1)
	var wg sync.WaitGroup
	results := l.NewTable()

	promises.ForEach(func(i, v lua.LValue) {
		if ud, ok := v.(*lua.LUserData); ok {
			if p, ok := ud.Value.(*Promise); ok {
				wg.Add(1)
				go func(index lua.LNumber) {
					defer wg.Done()

					result := l.NewTable()
					select {
					case r := <-p.resultChan:
						if r.Error != nil {
							result.RawSetString("status", lua.LString("rejected"))
							result.RawSetString("reason", lua.LString(r.Error.Error()))
						} else {
							result.RawSetString("status", lua.LString("fulfilled"))
							if r.Payload != nil {
								result.RawSetString("value", transcoder.GoToLua(l, r.Payload.Data()))
							} else {
								result.RawSetString("value", lua.LNil)
							}
						}
					case <-ctx.Done():
						result.RawSetString("status", lua.LString("rejected"))
						result.RawSetString("reason", lua.LString(ctx.Err().Error()))
					}
					results.RawSetInt(int(index), result)
				}(lua.LNumber(i))
			}
		}
	})

	go func() {
		wg.Wait()
		resultChan <- &runtime.Result{
			Payload: payload.NewPayload(results, payload.Lua),
		}
		close(resultChan)
	}()

	return createPromiseFromChan(l, ctx, resultChan)
}

// Helper functions

func getContextFromFirstPromise(l *lua.LState, promises *lua.LTable) context.Context {
	var ctx context.Context
	promises.ForEach(func(_, v lua.LValue) {
		if ud, ok := v.(*lua.LUserData); ok {
			if p, ok := ud.Value.(*Promise); ok && ctx == nil {
				ctx = p.ctx
			}
		}
	})
	return ctx
}

func createPromiseFromChan(l *lua.LState, ctx context.Context, resultChan chan *runtime.Result) int {
	promise := NewPromise(ctx, resultChan)
	ud := l.NewUserData()
	ud.Value = promise
	l.SetMetatable(ud, l.GetTypeMetatable("Promise"))
	l.Push(ud)
	return 1
}

func createResolvedPromise(l *lua.LState, value lua.LValue) int {
	resultChan := make(chan *runtime.Result, 1)
	resultChan <- &runtime.Result{
		Payload: payload.NewPayload(value, payload.Lua),
	}
	close(resultChan)
	return createPromiseFromChan(l, context.Background(), resultChan)
}

func createRejectedPromise(l *lua.LState, err error) int {
	resultChan := make(chan *runtime.Result, 1)
	resultChan <- &runtime.Result{Error: err}
	close(resultChan)
	return createPromiseFromChan(l, context.Background(), resultChan)
}

// Update Register function to include static methods
func Register(l *lua.LState) {
	mt := l.NewTypeMetatable("Promise")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), promiseMethods))

	// Register static methods
	promiseTable := l.NewTable()
	l.SetFuncs(promiseTable, promiseStaticMethods)
	l.SetGlobal("Promise", promiseTable)
}
