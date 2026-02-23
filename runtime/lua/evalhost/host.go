// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"context"
	"sync"

	"strings"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/engine"
	payloadconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"go.uber.org/zap"
)

// Note: The Compiler caches module definitions but has no persistent resources.
// All process lifecycle is managed by frame context cleanup.

const maxEvalSteps = 10000

// ImportLoader loads a library's Lua source code by registry ID.
// Returns the source code string or an error if not found.
type ImportLoader func(id registry.ID) (source string, err error)

// yieldResult holds the result of a dispatched yield
type yieldResult struct {
	data any
	err  error
	tag  uint64
}

// yieldCollector collects results from dispatched yields
type yieldCollector struct {
	done    chan struct{}
	results []yieldResult
	pending int
	mu      sync.Mutex
}

func newYieldCollector(count int) *yieldCollector {
	return &yieldCollector{
		results: make([]yieldResult, 0, count),
		done:    make(chan struct{}),
		pending: count,
	}
}

// CompleteYield implements dispatcher.ResultReceiver
func (c *yieldCollector) CompleteYield(tag uint64, data any, err error) {
	c.mu.Lock()
	c.results = append(c.results, yieldResult{tag: tag, data: data, err: err})
	c.pending--
	if c.pending == 0 {
		close(c.done)
	}
	c.mu.Unlock()
}

// Wait blocks until all yields are complete or context is canceled
func (c *yieldCollector) Wait(ctx context.Context) error {
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ToEvents converts collected results to process events
func (c *yieldCollector) ToEvents() []process.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	events := make([]process.Event, len(c.results))
	for i, r := range c.results {
		events[i] = process.Event{
			Type:  process.EventYieldComplete,
			Tag:   r.tag,
			Data:  r.data,
			Error: r.err,
		}
	}
	return events
}

// Host provides eval compilation and execution services.
type Host struct {
	log          *zap.Logger
	compiler     *Compiler
	importLoader ImportLoader
}

// NewHost creates a new eval host with a module provider.
func NewHost(log *zap.Logger, provider ModuleProvider) *Host {
	return &Host{
		log:      log,
		compiler: NewCompiler(provider),
	}
}

// WithImportLoader sets the import loader for loading library sources.
func (h *Host) WithImportLoader(loader ImportLoader) *Host {
	h.importLoader = loader
	return h
}

// Compile compiles Lua source into a reusable Program.
func (h *Host) Compile(_ context.Context, cmd CompileCmd) (*Program, error) {
	program, err := h.compiler.Compile(cmd)
	if err != nil {
		return nil, NewCompileError(err)
	}
	return program, nil
}

// Run compiles and executes Lua source in one step.
func (h *Host) Run(ctx context.Context, cmd RunCmd) (any, error) {
	// Compile the source
	program, err := h.compiler.Compile(CompileCmd{
		Source:       cmd.Source,
		Method:       cmd.Method,
		Modules:      cmd.Modules,
		Imports:      cmd.Imports,
		AllowClasses: cmd.AllowClasses,
	})
	if err != nil {
		return nil, NewCompileError(err)
	}

	// Create module binder that also injects imports and custom modules
	binder := h.createModuleBinder(program.Modules(), cmd.Imports, cmd.CustomModules)
	factory := engine.NewFactoryFromProto(program.Proto(), binder)
	proc, err := factory()
	if err != nil {
		return nil, NewCreateProcessError(err)
	}
	defer proc.Close()

	// Create fresh frame context for the eval process
	evalCtx, fc := ctxapi.OpenFrameContext(ctx)
	defer ctxapi.ReleaseFrameContext(fc)

	// Apply caller-provided context values
	if len(cmd.Context) > 0 {
		values := attrs.NewBagFrom(cmd.Context)
		if err := ctxapi.SetValues(evalCtx, values); err != nil {
			return nil, NewRunError(err)
		}
	}

	// Initialize with the method and arguments
	if err := proc.Init(evalCtx, cmd.Method, cmd.Args); err != nil {
		return nil, NewRunError(err)
	}

	// Get dispatcher for handling yields
	disp := dispatcher.GetDispatcher(ctx)

	// Auto-collect yields from imported modules
	allowedYields := h.collectModuleYields(program.Modules())

	// Merge with any explicitly provided yields (for overrides)
	for _, id := range cmd.AllowYields {
		allowedYields[id] = true
	}

	// Step until done
	var output process.StepOutput
	var events []process.Event
	stepCount := 0
	for {
		stepCount++
		if stepCount > maxEvalSteps {
			return nil, ErrMaxStepsExceeded
		}

		// Check for context cancellation
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		output.Reset()
		if err := proc.Step(events, &output); err != nil {
			return nil, NewRunError(err)
		}
		events = nil // Clear events after passing to Step

		// Handle yields
		if output.Count() > 0 {
			if disp == nil {
				return nil, NewRunError(ErrYieldsNotSupported)
			}

			// First pass: validate all yields and count valid ones
			var validYields []process.Yield
			var dispatchErr error

			output.ForEachYield(func(y process.Yield) {
				if dispatchErr != nil {
					return
				}

				yieldCmd := y.Cmd
				if yieldCmd == nil {
					return
				}

				// Check if this yield type is allowed
				if !allowedYields[yieldCmd.CmdID()] {
					dispatchErr = ErrYieldNotAllowed
					return
				}

				handler := disp.Dispatch(yieldCmd)
				if handler == nil {
					dispatchErr = NewNoHandlerError(yieldCmd.CmdID())
					return
				}

				validYields = append(validYields, y)
			})

			if dispatchErr != nil {
				return nil, NewRunError(dispatchErr)
			}

			if len(validYields) == 0 {
				continue
			}

			// Create collector with exact count and dispatch
			collector := newYieldCollector(len(validYields))
			for _, y := range validYields {
				handler := disp.Dispatch(y.Cmd)
				go func(tag uint64, c dispatcher.Command, h dispatcher.Handler) {
					err := h.Handle(evalCtx, c, tag, collector)
					if err != nil {
						collector.CompleteYield(tag, nil, err)
					}
				}(y.Tag, y.Cmd, handler)
			}

			// Wait for all yields to complete
			if err := collector.Wait(ctx); err != nil {
				return nil, err
			}

			// Convert results to events for next Step
			events = collector.ToEvents()
			continue
		}

		if output.IsDone() {
			result := output.Result()
			if result == nil {
				return nil, ErrNoResult
			}
			return result.Data(), nil
		}

		if output.IsIdle() {
			return nil, ErrProcessIdle
		}
	}
}

// GetCompiler returns the compiler for direct use.
func (h *Host) GetCompiler() *Compiler {
	return h.compiler
}

// collectModuleYields returns all yield command IDs from the given modules.
func (h *Host) collectModuleYields(modules []string) map[dispatcher.CommandID]bool {
	available := h.compiler.getModules()
	yields := make(map[dispatcher.CommandID]bool)

	for _, name := range modules {
		m, ok := available[name]
		if !ok {
			continue
		}
		for _, yt := range engine.ModuleYields(m) {
			yields[yt.CmdID] = true
		}
	}

	return yields
}

// createModuleBinder returns a ModuleBinder that loads modules, imports, and custom tables.
func (h *Host) createModuleBinder(modules []string, imports map[string]registry.ID, customModules map[string]any) engine.ModuleBinder {
	available := h.compiler.getModules()
	return func(l *lua.LState) error {
		// Load standard modules
		for _, name := range modules {
			m, ok := available[name]
			if !ok {
				continue
			}
			l.SetGlobal(m.Name, engine.ModuleValue(m))
		}

		// Load imports (library entries from registry)
		if h.importLoader != nil && len(imports) > 0 {
			for alias, id := range imports {
				source, err := h.importLoader(id)
				if err != nil {
					return NewImportError(alias, id, err)
				}

				// Compile and execute the library source to get its return value
				lv, err := h.loadLibrarySource(l, source, alias)
				if err != nil {
					return NewImportError(alias, id, err)
				}

				l.SetGlobal(alias, lv)
			}
		}

		// Inject custom modules (tables passed from caller)
		for name, v := range customModules {
			lv, err := payloadconv.GoToLua(v)
			if err != nil {
				return err
			}
			l.SetGlobal(name, lv)
		}

		return nil
	}
}

// loadLibrarySource compiles and executes a library source, returning the library's return value.
func (h *Host) loadLibrarySource(l *lua.LState, source, name string) (lua.LValue, error) {
	chunk, err := parse.Parse(strings.NewReader(source), name)
	if err != nil {
		return nil, err
	}

	proto, err := lua.CompileWithOptions(chunk, name, lua.CompileOptions{})
	if err != nil {
		return nil, err
	}

	fn := l.NewFunctionFromProto(proto)
	l.Push(fn)
	if err := l.PCall(0, 1, nil); err != nil {
		return nil, err
	}

	result := l.Get(-1)
	l.Pop(1)
	return result, nil
}
