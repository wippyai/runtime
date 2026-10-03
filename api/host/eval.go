// SPDX-License-Identifier: MPL-2.0

// Package host defines host boundaries shared across runtime services.
package host

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
)

var evalHostKey = &ctxapi.Key{Name: "host.eval"}

// EvalCompileMode selects how dynamic source is admitted. It is part of the
// program identity.
type EvalCompileMode uint8

const (
	// EvalCompileLite admits ordinary Lua source with explicit module and
	// capability injection.
	EvalCompileLite EvalCompileMode = iota
	// EvalCompileTyped additionally requires the source to pass strict type
	// checking.
	EvalCompileTyped
	// EvalCompileJIT is reserved; admission rejects it as unsupported.
	EvalCompileJIT
)

// EvalLinkMode controls the parent relationship at spawn admission.
type EvalLinkMode uint8

const (
	// EvalLinkRequired links and monitors the eval process to its caller.
	EvalLinkRequired EvalLinkMode = iota
	// EvalLinkMonitorOnly monitors without linking.
	EvalLinkMonitorOnly
	// EvalLinkDetached starts the eval process without a relationship; the
	// policy must allow it.
	EvalLinkDetached
)

// EvalSendMode defines how eval code obtains send authority.
type EvalSendMode uint8

const (
	// EvalSendObjectCapability allows addressing only PIDs the runtime handed
	// to the eval process: its parent, processes it spawned, senders of
	// messages it received and registry lookup results.
	EvalSendObjectCapability EvalSendMode = iota
	// EvalSendDenied forbids addressing any process.
	EvalSendDenied
	// EvalSendExplicitGrant allows addressing only EvalPolicy.SendTargets.
	EvalSendExplicitGrant
	// EvalSendPolicy is reserved; admission rejects it.
	EvalSendPolicy
)

// EvalCommand is a process command an eval policy may grant. Values of the
// process control commands are their command IDs; admission accepts only the
// ones this runtime supports and reports the rest as unsupported.
type EvalCommand uint8

const (
	// EvalCommandSpawn allows the eval process to spawn child processes,
	// bounded by EvalPolicy.MaxChildren.
	EvalCommandSpawn EvalCommand = 2
	// EvalCommandUpgrade is reserved for a granted self-upgrade; this runtime
	// does not support it and admission rejects it.
	EvalCommandUpgrade EvalCommand = 10
	// EvalCommandLookup allows the eval process to resolve registered process
	// names; a resolved PID is then addressable under the policy's send mode.
	EvalCommandLookup EvalCommand = 11
)

// EvalImport binds a registry library into eval source under an alias.
type EvalImport struct {
	Alias  string
	Source registry.ID
}

// EvalBinding binds a plain data value (nil, boolean, number, string or
// nested tables of those) as a global of the eval program. Each eval process
// receives its own copy.
type EvalBinding struct {
	Value any
	Name  string
}

// EvalPolicy is the admission policy of an eval program. Eval code does not
// inherit the caller's authority: every module, import, binding, command and
// send target it may use appears here.
type EvalPolicy struct {
	Modules       []string
	Imports       []EvalImport
	Bindings      []EvalBinding
	AllowClasses  []string
	AllowCommands []EvalCommand
	SendTargets   []pid.PID
	// MemoryLimitBytes, HeapReserveBytes and MailboxCapacity are accepted for
	// source compatibility; this runtime does not enforce them.
	MemoryLimitBytes uint64
	HeapReserveBytes uint64
	MaxSteps         uint64
	TickBudget       uint32
	MailboxCapacity  uint32
	MaxChildren      uint32
	CompileMode      EvalCompileMode
	SendMode         EvalSendMode
	AllowDetached    bool
}

// EvalCacheKey identifies a compiled eval program by hashes.
type EvalCacheKey struct {
	SourceHash [32]byte
	PolicyHash [32]byte
	Mode       EvalCompileMode
}

// IsZero reports whether k names no program.
func (k EvalCacheKey) IsZero() bool {
	return k == EvalCacheKey{}
}

// EvalProgram is a handle to a compiled eval program held by the eval host
// cache. It carries only identity.
type EvalProgram struct {
	Method string
	Key    EvalCacheKey
}

// IsZero reports whether p names no program.
func (p EvalProgram) IsZero() bool {
	return p.Key.IsZero()
}

// EvalCompileSpec admits source into the eval host cache. Compiling never
// executes the source.
type EvalCompileSpec struct {
	SourceCode string
	Method     string
	Policy     EvalPolicy
}

// EvalSpawnSpec starts an eval process from source or a compiled program.
// With a non-zero Program, SourceCode is ignored and the cached policy is
// authoritative; a non-empty Policy must match it.
type EvalSpawnSpec struct {
	SourceCode string
	Method     string
	Name       string
	Network    string
	Input      payload.Payloads
	Parent     pid.PID
	Policy     EvalPolicy
	Program    EvalProgram
	LinkMode   EvalLinkMode
}

// EvalHost compiles dynamic source and runs it as supervised processes.
// There is no synchronous run: callers monitor or link the spawned process.
type EvalHost interface {
	// Compile validates source and policy and caches the compiled program.
	Compile(ctx context.Context, spec EvalCompileSpec) (EvalProgram, error)
	// Spawn starts an eval process and returns its PID.
	Spawn(ctx context.Context, spec EvalSpawnSpec) (pid.PID, error)
	// Evict drops a compiled program from the cache. Running processes are
	// unaffected. A program that is not cached is ErrEvalProgramNotFound.
	Evict(ctx context.Context, program EvalProgram) error
}

// WithEvalHost stores the node-local EvalHost in the AppContext.
func WithEvalHost(ctx context.Context, h EvalHost) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	ac.With(evalHostKey, h)
	return ctx
}

// GetEvalHost returns the node-local EvalHost, or nil.
func GetEvalHost(ctx context.Context) EvalHost {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	h, _ := ac.Get(evalHostKey).(EvalHost)
	return h
}
