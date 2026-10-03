// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/go-lua/types/typ"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	apihost "github.com/wippyai/runtime/api/host"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	secapi "github.com/wippyai/runtime/api/security"
	luacode "github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	payloadconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	sysprocess "github.com/wippyai/runtime/system/process"
	syssecurity "github.com/wippyai/runtime/system/security"
)

// EvalProgramNamespace is the registry namespace of eval process frames. It
// names the frame only; eval programs are never registry entries.
const EvalProgramNamespace = "eval.program"

// Admitter is the eval host: it compiles dynamic source under an eval policy
// into a cached program and runs programs as supervised processes. Eval code
// does not inherit the caller's authority; its frame carries a scope built
// from the policy alone.
type Admitter struct {
	host    *Host
	manager process.Manager
	entries map[apihost.EvalCacheKey]*admitEntry
	sources map[sourceIdentity]apihost.EvalCacheKey
	lru     *list.List
	spawnOn pid.HostID

	detachedLifetime time.Duration
	capacity         int
	mu               sync.Mutex
}

// admitEntry is a compiled program with the policy it was admitted under.
type admitEntry struct {
	program   *Program
	elem      *list.Element
	frameName string
	modules   []*luaapi.ModuleDef
	imports   []admittedImport
	policy    apihost.EvalPolicy
	identity  sourceIdentity
	key       apihost.EvalCacheKey
}

// admittedImport is an imported library compiled at admission; spawns run
// exactly the revision the program was admitted with.
type admittedImport struct {
	proto *lua.FunctionProto
	alias string
	id    registry.ID
}

// importSource is an imported library's source loaded at admission.
type importSource struct {
	source string
	alias  string
	id     registry.ID
}

// sourceIdentity groups programs that differ only in imported library
// revisions, so a recompile against new libraries supersedes the old one.
type sourceIdentity struct {
	source [32]byte
	policy [32]byte
}

var _ apihost.EvalHost = (*Admitter)(nil)

// AdmitterOption configures an Admitter.
type AdmitterOption func(*Admitter)

// WithSpawnHost runs eval processes on host instead of the caller's host.
func WithSpawnHost(host pid.HostID) AdmitterOption {
	return func(a *Admitter) {
		a.spawnOn = host
	}
}

// WithDetachedLifetime bounds how long a detached eval may run; it has no
// owner to end it.
func WithDetachedLifetime(d time.Duration) AdmitterOption {
	return func(a *Admitter) {
		if d > 0 {
			a.detachedLifetime = d
		}
	}
}

// WithProgramCacheSize bounds the number of cached programs.
func WithProgramCacheSize(size int) AdmitterOption {
	return func(a *Admitter) {
		if size > 0 {
			a.capacity = size
		}
	}
}

// NewAdmitter returns an eval host that compiles with host's compiler and
// import loader and starts and stops processes through manager.
func NewAdmitter(host *Host, manager process.Manager, opts ...AdmitterOption) *Admitter {
	a := &Admitter{
		host:             host,
		manager:          manager,
		capacity:         DefaultEvalProgramCacheSize,
		detachedLifetime: DefaultDetachedEvalLifetime,
		entries:          make(map[apihost.EvalCacheKey]*admitEntry),
		sources:          make(map[sourceIdentity]apihost.EvalCacheKey),
		lru:              list.New(),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Compile implements apihost.EvalHost.
func (a *Admitter) Compile(ctx context.Context, spec apihost.EvalCompileSpec) (apihost.EvalProgram, error) {
	entry, err := a.compile(ctx, spec.SourceCode, spec.Method, spec.Policy)
	if err != nil {
		return apihost.EvalProgram{}, err
	}
	return apihost.EvalProgram{Method: entry.program.Method(), Key: entry.key}, nil
}

func (a *Admitter) compile(ctx context.Context, source, method string, raw apihost.EvalPolicy) (*admitEntry, error) {
	if err := validateSourceCode(source); err != nil {
		return nil, err
	}
	if err := validatePolicyShape(raw); err != nil {
		return nil, err
	}
	bindings, err := copyBindings(raw.Bindings)
	if err != nil {
		return nil, err
	}
	raw.Bindings = bindings
	policy := canonicalPolicy(raw)
	if err := validatePolicy(policy); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sources, imports, err := a.loadImports(policy.Imports)
	if err != nil {
		return nil, err
	}
	method = evalMethod(method)
	sourceHash := sha256.Sum256([]byte(method + "\x00" + source))
	basePolicy, err := hashPolicy(policy, [32]byte{})
	if err != nil {
		return nil, err
	}
	policyHash, err := hashPolicy(policy, imports)
	if err != nil {
		return nil, err
	}
	key := apihost.EvalCacheKey{SourceHash: sourceHash, PolicyHash: policyHash, Mode: policy.CompileMode}

	if entry := a.lookup(key); entry != nil {
		return entry, nil
	}

	available := a.host.compiler.getModules()
	if err := validateModuleClasses(policy, available); err != nil {
		return nil, err
	}
	admitted, err := compileImports(sources)
	if err != nil {
		return nil, err
	}
	typed := policy.CompileMode == apihost.EvalCompileTyped
	program, err := a.host.compiler.Compile(CompileCmd{
		Source:          source,
		Method:          method,
		Modules:         policy.Modules,
		AllowClasses:    policy.AllowClasses,
		ExplicitModules: true,
		AllowModules:    evalModuleNames,
		Strict:          typed,
		Globals:         admittedGlobals(policy, sources, typed),
	})
	if err != nil {
		return nil, NewCompileError(err)
	}

	entry := &admitEntry{
		modules:   modulesFor(program.Modules(), available),
		imports:   admitted,
		program:   program,
		policy:    policy,
		key:       key,
		identity:  sourceIdentity{source: sourceHash, policy: basePolicy},
		frameName: hex.EncodeToString(sourceHash[:8]) + hex.EncodeToString(policyHash[:8]),
	}
	return a.insert(entry), nil
}

// validateModuleClasses applies the eval module rule: a module is allowed by
// name, or it has an allowed class and no denied class.
func validateModuleClasses(policy apihost.EvalPolicy, available map[string]*luaapi.ModuleDef) error {
	allowed := evalModuleClasses(policy.AllowClasses)
	for _, name := range policy.Modules {
		if containsString(evalModuleNames, name) {
			continue
		}
		mod, ok := available[name]
		if !ok {
			continue // reported by the compiler with the available names
		}
		permitted := false
		for _, class := range mod.Class {
			if evalDeniedModuleClass(class) {
				return NewForbiddenClassError(name, class)
			}
			if containsString(allowed, class) {
				permitted = true
			}
		}
		if !permitted {
			return unsupported("module %q has no allowed class", name)
		}
	}
	return nil
}

// loadImports loads the current source of every imported library and
// fingerprints it, so a library change yields a new program.
func (a *Admitter) loadImports(imports []apihost.EvalImport) ([]importSource, [32]byte, error) {
	if len(imports) == 0 {
		return nil, [32]byte{}, nil
	}
	if a.host.importLoader == nil {
		return nil, [32]byte{}, unsupported("imports are not available")
	}
	sources := make([]importSource, 0, len(imports))
	h := sha256.New()
	for _, imp := range imports {
		source, err := a.host.importLoader(imp.Source)
		if err != nil {
			return nil, [32]byte{}, NewImportError(imp.Alias, imp.Source, err)
		}
		writeString(h, imp.Alias)
		writeString(h, source)
		sources = append(sources, importSource{source: source, alias: imp.Alias, id: imp.Source})
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sources, sum, nil
}

// compileImports compiles imported libraries once, at admission.
func compileImports(sources []importSource) ([]admittedImport, error) {
	if len(sources) == 0 {
		return nil, nil
	}
	out := make([]admittedImport, 0, len(sources))
	for _, src := range sources {
		chunk, err := parse.Parse(strings.NewReader(src.source), src.alias)
		if err != nil {
			return nil, NewImportError(src.alias, src.id, err)
		}
		proto, err := lua.CompileWithOptions(chunk, src.alias, lua.CompileOptions{})
		if err != nil {
			return nil, NewImportError(src.alias, src.id, err)
		}
		out = append(out, admittedImport{proto: proto, alias: src.alias, id: src.id})
	}
	return out, nil
}

// admittedGlobals declares the program's bindings and imports to the type
// checker; only typed compilation checks them.
func admittedGlobals(policy apihost.EvalPolicy, sources []importSource, typed bool) map[string]typ.Type {
	if !typed || len(policy.Bindings)+len(sources) == 0 {
		return nil
	}
	globals := make(map[string]typ.Type, len(policy.Bindings)+len(sources))
	for _, b := range policy.Bindings {
		globals[b.Name] = typ.Any
	}
	for _, src := range sources {
		globals[src.alias] = libraryExportType(src)
	}
	return globals
}

// libraryExportType is the type of the value an imported library returns.
func libraryExportType(src importSource) typ.Type {
	cfg := luacode.DefaultTypeCheckConfig()
	cfg.Enabled = true
	checker := luacode.NewTypeChecker(cfg, nil)
	manifest, _, err := checker.Check(src.source, src.alias, nil)
	if err != nil || manifest == nil || manifest.Export == nil {
		return typ.Any
	}
	return manifest.Export
}

func modulesFor(names []string, available map[string]*luaapi.ModuleDef) []*luaapi.ModuleDef {
	out := make([]*luaapi.ModuleDef, 0, len(names))
	for _, name := range names {
		if mod := available[name]; mod != nil {
			out = append(out, mod)
		}
	}
	return out
}

func (a *Admitter) lookup(key apihost.EvalCacheKey) *admitEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry := a.entries[key]
	if entry != nil {
		a.lru.MoveToFront(entry.elem)
	}
	return entry
}

// insert caches entry, superseding an older program of the same source
// identity and evicting the least recently used program over capacity. A
// concurrent compile of the same key keeps the first entry.
func (a *Admitter) insert(entry *admitEntry) *admitEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	if existing := a.entries[entry.key]; existing != nil {
		a.lru.MoveToFront(existing.elem)
		return existing
	}
	if prev, ok := a.sources[entry.identity]; ok && prev != entry.key {
		a.removeLocked(prev)
	}
	entry.elem = a.lru.PushFront(entry.key)
	a.entries[entry.key] = entry
	a.sources[entry.identity] = entry.key
	for len(a.entries) > a.capacity {
		oldest := a.lru.Back()
		a.removeLocked(oldest.Value.(apihost.EvalCacheKey))
	}
	return entry
}

func (a *Admitter) removeLocked(key apihost.EvalCacheKey) bool {
	entry := a.entries[key]
	if entry == nil {
		return false
	}
	a.lru.Remove(entry.elem)
	delete(a.entries, key)
	if a.sources[entry.identity] == key {
		delete(a.sources, entry.identity)
	}
	return true
}

// Evict implements apihost.EvalHost. Running eval processes keep their
// program.
func (a *Admitter) Evict(_ context.Context, program apihost.EvalProgram) (bool, error) {
	if program.IsZero() {
		return false, apihost.ErrEvalProgramNotFound
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.removeLocked(program.Key), nil
}

// Spawn implements apihost.EvalHost.
func (a *Admitter) Spawn(ctx context.Context, spec apihost.EvalSpawnSpec) (pid.PID, error) {
	if err := ctx.Err(); err != nil {
		return pid.PID{}, err
	}
	entry, err := a.programFor(ctx, spec)
	if err != nil {
		return pid.PID{}, err
	}
	policy := entry.policy

	link, monitor, err := evalLinkMode(spec.LinkMode, spec.Parent, policy)
	if err != nil {
		return pid.PID{}, err
	}
	if spec.LinkMode == apihost.EvalLinkDetached {
		if err := detachableFrom(ctx); err != nil {
			return pid.PID{}, err
		}
	}

	hostID := a.spawnOn
	if hostID == "" {
		hostID = spec.Parent.Host
	}
	if hostID == "" {
		return pid.PID{}, apihost.ErrEvalParentRequired
	}

	options := attrs.NewBag()
	if link || monitor {
		options.Set(process.ProcessParentKey, spec.Parent)
	}
	if link {
		options.Set(process.ProcessLinkKey, true)
	}
	if monitor {
		options.Set(process.ProcessMonitorKey, true)
	}
	if spec.Name != "" {
		options.Set(process.ProcessNameKey, spec.Name)
	}
	if spec.Network != "" {
		options.Set(netapi.OptionKeyNetwork, spec.Network)
	}

	// Unless detached, the eval is owned by the execution that spawned it:
	// it never outlives that execution, however the execution ends. A
	// detached eval has no owner, so its lifetime is bounded instead.
	var lifetime time.Duration
	frame := evalFrame(entry.frameName, spec.Parent, policy)
	if spec.LinkMode == apihost.EvalLinkDetached {
		lifetime = a.detachedLifetime
		// What a detached eval starts ends with it, so its lifetime bounds
		// them too.
		frame = append(frame, process.OwnsChildrenPair())
	} else {
		options.Set(process.ProcessOwnedKey, true)
	}

	start := &process.Start{
		HostID:  hostID,
		Source:  registry.ID{NS: EvalProgramNamespace, Name: entry.frameName},
		Input:   spec.Input,
		Options: options,
		// The eval starts from a clean frame: only values meant to cross
		// into another process, such as trace context, follow it.
		Context: append(frame, ctxapi.PropagatorPairs(ctx)...),
		Admission: &process.Admission{
			Factory: engine.NewFactory(engine.FactoryConfig{
				Proto:         entry.program.Proto(),
				ModuleBinders: []engine.ModuleBinder{a.binder(entry)},
				Budgets: luaapi.ExecutionBudgets{
					TickBudget:    int64(policy.TickBudget),
					TickBudgetSet: true,
					MaxSteps:      policy.MaxSteps,
					MaxStepsSet:   true,
				},
				Lifetime: lifetime,
			}),
			Meta: process.Meta{Method: entry.program.Method()},
		},
	}
	child, err := a.manager.Start(ctx, start)
	if err != nil && a.spawnOn == "" && errors.Is(err, sysprocess.ErrInvalidHost) {
		return child, fmt.Errorf("eval runs on the caller's host %s, which cannot run processes; configure lua.eval.spawn_host: %w", hostID, err)
	}
	return child, err
}

// programFor returns the program to spawn: a cached program, whose policy a
// non-empty spec policy must match, or source compiled under the spec policy.
func (a *Admitter) programFor(ctx context.Context, spec apihost.EvalSpawnSpec) (*admitEntry, error) {
	if spec.Program.IsZero() {
		return a.compile(ctx, spec.SourceCode, spec.Method, spec.Policy)
	}
	entry := a.lookup(spec.Program.Key)
	if entry == nil {
		return nil, apihost.ErrEvalProgramNotFound
	}
	if !policyIsZero(spec.Policy) {
		policy := canonicalPolicy(spec.Policy)
		bindings, err := copyBindings(policy.Bindings)
		if err != nil {
			return nil, err
		}
		policy.Bindings = bindings
		requested, err := hashPolicy(policy, [32]byte{})
		if err != nil {
			return nil, err
		}
		if requested != entry.identity.policy {
			return nil, apihost.ErrEvalPolicyMismatch
		}
	}
	return entry, nil
}

func evalLinkMode(mode apihost.EvalLinkMode, parent pid.PID, policy apihost.EvalPolicy) (link, monitor bool, err error) {
	switch mode {
	case apihost.EvalLinkDetached:
		if !policy.AllowDetached {
			return false, false, apihost.ErrEvalDetachedDenied
		}
		return false, false, nil
	case apihost.EvalLinkMonitorOnly:
		if parent.UniqID == "" {
			return false, false, apihost.ErrEvalParentRequired
		}
		return false, true, nil
	default:
		if parent.UniqID == "" {
			return false, false, apihost.ErrEvalParentRequired
		}
		return true, true, nil
	}
}

// detachableFrom reports whether the execution in ctx may start an eval that
// outlives it: only a process that is not itself owned may. A function call
// never is, and an owned process stays contained.
func detachableFrom(ctx context.Context) error {
	scope := process.GetExecutionScope(ctx)
	switch {
	case scope == nil:
		return fmt.Errorf("%w: caller has no execution scope", apihost.ErrEvalDetachedDenied)
	case scope.Kind() == process.ExecutionFunction:
		return fmt.Errorf("%w: a function call cannot outlive its evals", apihost.ErrEvalDetachedDenied)
	case process.IsOwned(ctx):
		return fmt.Errorf("%w: an owned process cannot detach", apihost.ErrEvalDetachedDenied)
	}
	return nil
}

// evalFrame returns the frame pairs that confine an eval process: its own
// actor and scope, send grants in object-capability mode, and a child limit
// when it may spawn.
func evalFrame(name string, parent pid.PID, policy apihost.EvalPolicy) []ctxapi.Pair {
	actor := secapi.Actor{ID: "eval:" + name}
	if parent.UniqID != "" {
		actor.Meta = attrs.Bag{"parent": parent.String()}
	}
	pairs := []ctxapi.Pair{
		secapi.ActorPair(actor),
		secapi.ScopePair(syssecurity.NewScope([]secapi.Policy{newEvalPolicy(name, policy)})),
	}
	if policy.SendMode == apihost.EvalSendObjectCapability {
		var seed []pid.PID
		if parent.UniqID != "" {
			seed = append(seed, parent)
		}
		pairs = append(pairs, secapi.ProcessSendGrantsPair(secapi.NewProcessSendGrants(seed...)))
	}
	if policy.MaxChildren > 0 {
		pairs = append(pairs, process.ChildSlotsPair(process.NewChildSlots(int(policy.MaxChildren))))
	}
	return pairs
}

// binder loads the program's admitted modules, imports and bindings into a
// new process state. Imports run as initializers of the process, before the
// program and under the same budget.
func (a *Admitter) binder(entry *admitEntry) engine.ModuleBinder {
	return func(l *lua.LState) error {
		for _, mod := range entry.modules {
			l.SetGlobal(mod.Name, engine.ModuleValue(mod))
		}
		for _, imp := range entry.imports {
			alias := imp.alias
			engine.DeferInitializer(l, engine.Initializer{
				Fn:   l.NewFunctionFromProto(imp.proto),
				Done: func(result lua.LValue) { l.SetGlobal(alias, result) },
			})
		}
		for _, b := range entry.policy.Bindings {
			v, err := payloadconv.GoToLua(b.Value)
			if err != nil {
				return err
			}
			l.SetGlobal(b.Name, v)
		}
		return nil
	}
}
