// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lru "github.com/wippyai/runtime/internal/cache"
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
	programCache *lru.Cache[string, *Program]
}

// HostConfig configures optional Host behavior.
type HostConfig struct {
	// CacheSize bounds the number of compiled eval programs kept in the LRU
	// compile cache. Zero disables caching (every run recompiles).
	CacheSize int
	// CacheTTL optionally expires cached programs after a duration. Zero keeps
	// entries until evicted by the size bound.
	CacheTTL time.Duration
}

// HostOption configures a Host.
type HostOption func(*Host)

// WithProgramCache enables the compiled-program LRU cache. A size of zero leaves
// caching disabled.
func WithProgramCache(cfg HostConfig) HostOption {
	return func(h *Host) {
		if cfg.CacheSize <= 0 {
			return
		}
		opts := []lru.Option{lru.WithCapacity(cfg.CacheSize)}
		if cfg.CacheTTL > 0 {
			opts = append(opts, lru.WithTTL(cfg.CacheTTL))
		}
		h.programCache = lru.New[string, *Program](opts...)
	}
}

// NewHost creates a new eval host with a module provider.
func NewHost(log *zap.Logger, provider ModuleProvider, opts ...HostOption) *Host {
	h := &Host{
		log:      log,
		compiler: NewCompiler(provider),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// WithImportLoader sets the import loader for loading library sources.
func (h *Host) WithImportLoader(loader ImportLoader) *Host {
	h.importLoader = loader
	return h
}

// Compile compiles Lua source into a reusable Program.
func (h *Host) Compile(_ context.Context, cmd CompileCmd) (*Program, error) {
	program, err := h.compile(cmd)
	if err != nil {
		return nil, NewCompileError(err)
	}
	return program, nil
}

// compile returns a compiled Program for the command, served from the LRU
// compile cache when enabled. The compiled bytecode depends only on the source,
// method, allowed modules and allowed classes; imports are bound at run time, so
// they are not part of the cache key.
func (h *Host) compile(cmd CompileCmd) (*Program, error) {
	if h.programCache == nil {
		return h.compiler.Compile(cmd)
	}

	key := programCacheKey(cmd)
	if program, ok := h.programCache.Get(key); ok {
		return program, nil
	}

	program, err := h.compiler.Compile(cmd)
	if err != nil {
		return nil, err
	}
	_ = h.programCache.Set(key, program)
	return program, nil
}

// programCacheKey derives a collision-resistant key from the inputs that affect
// compiled bytecode.
func programCacheKey(cmd CompileCmd) string {
	hsh := sha256.New()
	hsh.Write([]byte(cmd.Source))
	hsh.Write([]byte{0})
	hsh.Write([]byte(cmd.Method))
	hsh.Write([]byte{0})

	mods := append([]string(nil), cmd.Modules...)
	sort.Strings(mods)
	for _, m := range mods {
		hsh.Write([]byte(m))
		hsh.Write([]byte{0})
	}
	hsh.Write([]byte{1})

	classes := append([]string(nil), cmd.AllowClasses...)
	sort.Strings(classes)
	for _, c := range classes {
		hsh.Write([]byte(c))
		hsh.Write([]byte{0})
	}
	return hex.EncodeToString(hsh.Sum(nil))
}

// Run compiles and executes Lua source in one step.
func (h *Host) Run(ctx context.Context, cmd RunCmd) (any, error) {
	// Compile the source (served from the LRU compile cache when enabled).
	program, err := h.compile(CompileCmd{
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
	binder := h.createModuleBinder(program.Modules(), cmd.Imports, cmd.ImportModules, cmd.CustomModules)
	factory := engine.NewFactoryFromProto(program.Proto(), binder)
	proc, err := factory()
	if err != nil {
		return nil, NewCreateProcessError(err)
	}
	defer proc.Close()

	// Eval always owns an isolated execution frame.
	evalCtx, fc := ctxapi.ForkFrameContext(ctx)
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

	// Auto-collect yields from the eval program's modules and from any
	// privileged modules granted to imports, so granted capabilities that yield
	// (e.g. funcs) remain usable inside the import.
	yieldModules := append([]string(nil), program.Modules()...)
	for _, mods := range cmd.ImportModules {
		yieldModules = append(yieldModules, mods...)
	}
	allowedYields := h.collectModuleYields(yieldModules)

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

			// Freeze parent frame for concurrent yield handlers.
			fc.Seal()

			// Create collector with exact count and dispatch
			collector := newYieldCollector(len(validYields))
			for _, y := range validYields {
				handler := disp.Dispatch(y.Cmd)
				yieldCtx, yieldFC := ctxapi.ForkFrameContext(evalCtx)
				go func(tag uint64, c dispatcher.Command, h dispatcher.Handler, callCtx context.Context, callFC ctxapi.FrameContext) {
					defer ctxapi.ReleaseFrameContext(callFC)
					err := h.Handle(callCtx, c, tag, collector)
					if err != nil {
						collector.CompleteYield(tag, nil, err)
					}
				}(y.Tag, y.Cmd, handler, yieldCtx, yieldFC)
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
func (h *Host) createModuleBinder(modules []string, imports map[string]registry.ID, importModules map[string][]string, customModules map[string]any) engine.ModuleBinder {
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

				// An import granted privileged modules runs in its own
				// environment so those modules stay reachable only inside the
				// import's code, never the eval'd program's globals.
				granted := importModules[alias]
				lv, err := h.loadLibrarySource(l, source, alias, granted, available)
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

// loadLibrarySource compiles and executes a library source, returning the
// library's return value. When granted is non-empty the library runs under a
// private environment that exposes those modules (and a scoped require for
// them) so they remain reachable only inside the import, never in the eval'd
// program's globals.
func (h *Host) loadLibrarySource(l *lua.LState, source, name string, granted []string, available map[string]*luaapi.ModuleDef) (lua.LValue, error) {
	chunk, err := parse.Parse(strings.NewReader(source), name)
	if err != nil {
		return nil, err
	}

	proto, err := lua.CompileWithOptions(chunk, name, lua.CompileOptions{})
	if err != nil {
		return nil, err
	}

	fn := l.NewFunctionFromProto(proto)

	if len(granted) > 0 {
		env, err := privilegedImportEnv(l, granted, available)
		if err != nil {
			return nil, err
		}
		fn.Env = env
	}

	l.Push(fn)
	if err := l.PCall(0, 1, nil); err != nil {
		return nil, err
	}

	result := l.Get(-1)
	l.Pop(1)
	return result, nil
}

// privilegedImportEnv builds a scoped global environment for a privileged
// import. It exposes only the granted modules (as globals and via a scoped
// require) and falls back to the eval program's base globals for everything
// else. Granted modules are not written into the shared eval globals, so the
// eval'd program cannot reach them.
func privilegedImportEnv(l *lua.LState, granted []string, available map[string]*luaapi.ModuleDef) (*lua.LTable, error) {
	base := l.Get(lua.GlobalsIndex).(*lua.LTable)

	env := l.NewTable()
	mt := l.NewTable()
	mt.RawSetString("__index", base)
	l.SetMetatable(env, mt)

	resolved := make(map[string]lua.LValue, len(granted))
	for _, name := range granted {
		m, ok := available[name]
		if !ok {
			return nil, NewModuleNotAvailableError(name)
		}
		v := engine.ModuleValue(m)
		env.RawSetString(name, v)
		resolved[name] = v
	}

	env.RawSetString("require", l.NewFunction(func(s *lua.LState) int {
		name := s.CheckString(1)
		if v, ok := resolved[name]; ok {
			s.Push(v)
			return 1
		}
		if v := base.RawGetString(name); v != lua.LNil {
			s.Push(v)
			return 1
		}
		s.RaiseError("module '%s' not found", name)
		return 0
	}))

	return env, nil
}
