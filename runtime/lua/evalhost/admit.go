// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	apihost "github.com/wippyai/runtime/api/host"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	secapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/engine"
	sysprocess "github.com/wippyai/runtime/system/process"
	syssecurity "github.com/wippyai/runtime/system/security"
)

// EvalProgramNamespace is the registry namespace of eval process frames. It
// names the frame only; eval programs are never registry entries.
const EvalProgramNamespace = "eval.program"

// ProcessStarter starts processes; process.Manager implements it.
type ProcessStarter interface {
	Start(ctx context.Context, start *process.Start) (pid.PID, error)
}

// Admitter is the eval host: it compiles dynamic source under an eval policy
// into a cached program and runs programs as supervised processes. Eval code
// does not inherit the caller's authority; its frame carries a scope built
// from the policy alone.
type Admitter struct {
	host    *Host
	starter ProcessStarter
	entries map[apihost.EvalCacheKey]*admitEntry
	sources map[sourceIdentity]apihost.EvalCacheKey
	lru     *list.List
	spawnOn pid.HostID

	capacity int
	mu       sync.Mutex
}

// admitEntry is a compiled program with the policy it was admitted under.
type admitEntry struct {
	program  *Program
	elem     *list.Element
	policy   apihost.EvalPolicy
	identity sourceIdentity
	key      apihost.EvalCacheKey
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

// WithProgramCacheSize bounds the number of cached programs.
func WithProgramCacheSize(size int) AdmitterOption {
	return func(a *Admitter) {
		if size > 0 {
			a.capacity = size
		}
	}
}

// NewAdmitter returns an eval host that compiles with host's compiler and
// import loader and starts processes through starter.
func NewAdmitter(host *Host, starter ProcessStarter, opts ...AdmitterOption) *Admitter {
	a := &Admitter{
		host:     host,
		starter:  starter,
		capacity: DefaultEvalProgramCacheSize,
		entries:  make(map[apihost.EvalCacheKey]*admitEntry),
		sources:  make(map[sourceIdentity]apihost.EvalCacheKey),
		lru:      list.New(),
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
	policy := canonicalPolicy(raw)
	if err := validatePolicy(policy); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	imports, err := a.importFingerprint(policy.Imports)
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

	if err := a.validateModuleClasses(policy); err != nil {
		return nil, err
	}
	program, err := a.host.compiler.Compile(CompileCmd{
		Source:          source,
		Method:          method,
		Modules:         policy.Modules,
		AllowClasses:    policy.AllowClasses,
		ExplicitModules: true,
		AllowModules:    evalModuleNames,
		Strict:          policy.CompileMode == apihost.EvalCompileTyped,
	})
	if err != nil {
		return nil, NewCompileError(err)
	}

	entry := &admitEntry{
		program:  program,
		policy:   policy,
		key:      key,
		identity: sourceIdentity{source: sourceHash, policy: basePolicy},
	}
	return a.insert(entry), nil
}

// validateModuleClasses applies the eval module rule: a module is allowed by
// name, or it has an allowed class and no denied class.
func (a *Admitter) validateModuleClasses(policy apihost.EvalPolicy) error {
	available := a.host.compiler.getModules()
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

// importFingerprint hashes the current source of every imported library, so
// a library change yields a new program.
func (a *Admitter) importFingerprint(imports []apihost.EvalImport) ([32]byte, error) {
	if len(imports) == 0 {
		return [32]byte{}, nil
	}
	if a.host.importLoader == nil {
		return [32]byte{}, unsupported("imports are not available")
	}
	h := sha256.New()
	for _, imp := range imports {
		source, err := a.host.importLoader(imp.Source)
		if err != nil {
			return [32]byte{}, NewImportError(imp.Alias, imp.Source, err)
		}
		writeString(h, imp.Alias)
		writeString(h, source)
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
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
	entry, err := a.programFor(ctx, spec)
	if err != nil {
		return pid.PID{}, err
	}
	policy := entry.policy

	link, monitor, err := evalLinkMode(spec.LinkMode, spec.Parent, policy)
	if err != nil {
		return pid.PID{}, err
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

	frameName := hex.EncodeToString(entry.key.SourceHash[:8]) + hex.EncodeToString(entry.key.PolicyHash[:8])
	start := &process.Start{
		HostID:  hostID,
		Source:  registry.ID{NS: EvalProgramNamespace, Name: frameName},
		Input:   spec.Input,
		Options: options,
		Context: evalFrame(frameName, spec.Parent, policy),
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
			}),
			Meta: process.Meta{Method: entry.program.Method()},
		},
	}
	child, err := a.starter.Start(ctx, start)
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
	a.mu.Lock()
	entry := a.entries[spec.Program.Key]
	a.mu.Unlock()
	if entry == nil {
		return nil, apihost.ErrEvalProgramNotFound
	}
	if !policyIsZero(spec.Policy) {
		requested, err := hashPolicy(canonicalPolicy(spec.Policy), [32]byte{})
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

// binder loads the program's modules, imports and bindings into a new
// process state.
func (a *Admitter) binder(entry *admitEntry) engine.ModuleBinder {
	policy := entry.policy
	var imports map[string]registry.ID
	if len(policy.Imports) > 0 {
		imports = make(map[string]registry.ID, len(policy.Imports))
		for _, imp := range policy.Imports {
			imports[imp.Alias] = imp.Source
		}
	}
	var bindings map[string]any
	if len(policy.Bindings) > 0 {
		bindings = make(map[string]any, len(policy.Bindings))
		for _, b := range policy.Bindings {
			bindings[b.Name] = b.Value
		}
	}
	return a.host.createModuleBinder(entry.program.Modules(), imports, nil, bindings)
}
