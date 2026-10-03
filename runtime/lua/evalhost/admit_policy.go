// SPDX-License-Identifier: MPL-2.0

package evalhost

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"math"
	"sort"
	"strings"
	"time"

	apihost "github.com/wippyai/runtime/api/host"
	"github.com/wippyai/runtime/api/pid"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
)

// Eval admission limits; values match the v2 eval host.
const (
	maxEvalSourceBytes = 256 << 10

	maxEvalModules       = 32
	maxEvalImports       = 32
	maxEvalBindings      = 64
	maxEvalAllowClasses  = 16
	maxEvalAllowCommands = 16
	maxEvalSendTargets   = 128

	maxEvalPolicySingleStringBytes = 1024
	maxEvalPolicyStringBytes       = 32 << 10

	defaultEvalTickBudget      uint32 = 512
	maxEvalTickBudget          uint32 = 1 << 16
	defaultEvalMaxSteps        uint64 = 4096
	maxEvalMaxSteps            uint64 = 1 << 20
	defaultEvalMailboxCapacity uint32 = 64
	maxEvalMailboxCapacity     uint32 = 4096
	maxEvalMaxChildren         uint32 = 64

	// DefaultEvalProgramCacheSize bounds compiled eval programs per host.
	DefaultEvalProgramCacheSize = 1024

	// DefaultDetachedEvalLifetime bounds how long a detached eval may run.
	DefaultDetachedEvalLifetime = time.Hour

	defaultEvalMethod = "main"
)

// evalModuleNames are modules eval code may load regardless of their class.
var evalModuleNames = []string{"process"}

// evalBaseModuleClasses are module classes eval code may load by default.
var evalBaseModuleClasses = []string{
	luaapi.ClassDeterministic,
	luaapi.ClassEncoding,
	luaapi.ClassTime,
	luaapi.ClassNondeterministic,
	luaapi.ClassIO,
}

func evalDeniedModuleClass(class string) bool {
	switch class {
	case luaapi.ClassProcess, luaapi.ClassStorage, luaapi.ClassNetwork:
		return true
	default:
		return false
	}
}

func validateSourceCode(source string) error {
	if source == "" {
		return apihost.ErrEvalSourceRequired
	}
	if len(source) > maxEvalSourceBytes {
		return apihost.ErrEvalSourceTooLarge
	}
	return nil
}

// validatePolicy checks a canonical policy.
func validatePolicy(policy apihost.EvalPolicy) error {
	if err := validatePolicyShape(policy); err != nil {
		return err
	}
	for _, class := range policy.AllowClasses {
		if class == "" || evalDeniedModuleClass(class) {
			return unsupported("allow_classes may not include %q", class)
		}
	}
	for _, module := range policy.Modules {
		if module == "" || strings.Contains(module, ":") {
			return unsupported("invalid module name %q", module)
		}
	}
	if policy.CompileMode > apihost.EvalCompileTyped {
		return unsupported("unknown compile mode")
	}
	if policy.TickBudget == 0 || policy.TickBudget > maxEvalTickBudget ||
		policy.MaxSteps == 0 || policy.MaxSteps > maxEvalMaxSteps ||
		policy.MailboxCapacity == 0 || policy.MailboxCapacity > maxEvalMailboxCapacity {
		return unsupported("limits out of range")
	}
	if err := validateImports(policy); err != nil {
		return err
	}
	if err := validateBindings(policy); err != nil {
		return err
	}
	if policy.MaxChildren > maxEvalMaxChildren {
		return unsupported("max_children exceeds %d", maxEvalMaxChildren)
	}
	hasSpawn := false
	for _, command := range policy.AllowCommands {
		switch command {
		case apihost.EvalCommandSpawn:
			hasSpawn = true
			if policy.MaxChildren == 0 {
				return unsupported("command spawn requires max_children")
			}
		case apihost.EvalCommandUpgrade:
			return unsupported("command upgrade is not supported by this runtime")
		default:
			return unsupported("unknown command")
		}
	}
	if policy.MaxChildren != 0 && !hasSpawn {
		return unsupported("max_children requires command spawn")
	}
	if policy.SendMode == apihost.EvalSendPolicy {
		return unsupported("send mode policy is not supported")
	}
	if len(policy.SendTargets) != 0 && policy.SendMode != apihost.EvalSendExplicitGrant {
		return unsupported("send_targets requires send mode explicit")
	}
	for _, target := range policy.SendTargets {
		if target.UniqID == "" {
			return unsupported("send_targets contains an empty pid")
		}
	}
	return nil
}

func unsupported(format string, args ...any) error {
	return fmt.Errorf("%w: %s", apihost.ErrEvalPolicyUnsupported, fmt.Sprintf(format, args...))
}

func validatePolicyShape(policy apihost.EvalPolicy) error {
	if len(policy.Modules) > maxEvalModules ||
		len(policy.Imports) > maxEvalImports ||
		len(policy.Bindings) > maxEvalBindings ||
		len(policy.AllowClasses) > maxEvalAllowClasses ||
		len(policy.AllowCommands) > maxEvalAllowCommands ||
		len(policy.SendTargets) > maxEvalSendTargets {
		return unsupported("policy exceeds size limits")
	}
	total := 0
	strs := make([]string, 0, len(policy.Modules)+3*len(policy.Imports)+len(policy.Bindings)+len(policy.AllowClasses))
	strs = append(strs, policy.Modules...)
	for _, imp := range policy.Imports {
		strs = append(strs, imp.Alias, imp.Source.NS, imp.Source.Name)
	}
	for _, binding := range policy.Bindings {
		strs = append(strs, binding.Name)
	}
	strs = append(strs, policy.AllowClasses...)
	for _, s := range strs {
		if len(s) > maxEvalPolicySingleStringBytes {
			return unsupported("policy string exceeds %d bytes", maxEvalPolicySingleStringBytes)
		}
		total += len(s)
		if total > maxEvalPolicyStringBytes {
			return unsupported("policy strings exceed %d bytes", maxEvalPolicyStringBytes)
		}
	}
	return nil
}

func validateImports(policy apihost.EvalPolicy) error {
	for i, imp := range policy.Imports {
		if !ValidBindingName(imp.Alias) || imp.Source.NS == "" || imp.Source.Name == "" {
			return unsupported("invalid import %q", imp.Alias)
		}
		if i > 0 && policy.Imports[i-1].Alias == imp.Alias {
			return unsupported("duplicate import %q", imp.Alias)
		}
		for _, module := range policy.Modules {
			if module == imp.Alias {
				return unsupported("import %q shadows a module", imp.Alias)
			}
		}
	}
	return nil
}

func validateBindings(policy apihost.EvalPolicy) error {
	for i, binding := range policy.Bindings {
		if !ValidBindingName(binding.Name) {
			return apihost.ErrEvalBindingInvalid
		}
		if i > 0 && policy.Bindings[i-1].Name == binding.Name {
			return apihost.ErrEvalBindingInvalid
		}
		for _, module := range policy.Modules {
			if module == binding.Name {
				return apihost.ErrEvalBindingInvalid
			}
		}
		for _, imp := range policy.Imports {
			if imp.Alias == binding.Name {
				return apihost.ErrEvalBindingInvalid
			}
		}
	}
	return nil
}

// ValidBindingName reports whether name is a Lua identifier an eval program
// can receive a binding or import under.
func ValidBindingName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		letter := c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
		if letter || i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

// canonicalPolicy sorts and de-duplicates a policy and applies defaults, so
// equivalent policies share one program.
func canonicalPolicy(policy apihost.EvalPolicy) apihost.EvalPolicy {
	out := policy
	out.Modules = uniqueSortedStrings(policy.Modules)
	out.Imports = append([]apihost.EvalImport(nil), policy.Imports...)
	sort.SliceStable(out.Imports, func(i, j int) bool { return out.Imports[i].Alias < out.Imports[j].Alias })
	out.Bindings = append([]apihost.EvalBinding(nil), policy.Bindings...)
	sort.SliceStable(out.Bindings, func(i, j int) bool { return out.Bindings[i].Name < out.Bindings[j].Name })
	out.AllowClasses = uniqueSortedStrings(policy.AllowClasses)
	out.AllowCommands = uniqueSortedCommands(policy.AllowCommands)
	out.SendTargets = uniqueSortedPIDs(policy.SendTargets)
	if out.TickBudget == 0 {
		out.TickBudget = defaultEvalTickBudget
	}
	if out.MaxSteps == 0 {
		out.MaxSteps = defaultEvalMaxSteps
	}
	if out.MailboxCapacity == 0 {
		out.MailboxCapacity = defaultEvalMailboxCapacity
	}
	return out
}

// policyIsZero reports whether no policy field was set.
func policyIsZero(policy apihost.EvalPolicy) bool {
	return policy.CompileMode == 0 &&
		len(policy.Modules) == 0 &&
		len(policy.Imports) == 0 &&
		len(policy.Bindings) == 0 &&
		len(policy.AllowClasses) == 0 &&
		len(policy.AllowCommands) == 0 &&
		policy.SendMode == 0 &&
		len(policy.SendTargets) == 0 &&
		policy.MemoryLimitBytes == 0 &&
		policy.HeapReserveBytes == 0 &&
		policy.TickBudget == 0 &&
		policy.MaxSteps == 0 &&
		policy.MailboxCapacity == 0 &&
		policy.MaxChildren == 0 &&
		!policy.AllowDetached
}

// hashPolicy hashes a canonical policy together with the fingerprint of the
// imported library sources.
func hashPolicy(policy apihost.EvalPolicy, imports [32]byte) ([32]byte, error) {
	h := sha256.New()
	writeUint(h, uint64(policy.CompileMode))
	writeUint(h, uint64(policy.SendMode))
	writeUint(h, policy.MemoryLimitBytes)
	writeUint(h, policy.HeapReserveBytes)
	writeUint(h, policy.MaxSteps)
	writeUint(h, uint64(policy.TickBudget))
	writeUint(h, uint64(policy.MailboxCapacity))
	writeUint(h, uint64(policy.MaxChildren))
	if policy.AllowDetached {
		writeUint(h, 1)
	} else {
		writeUint(h, 0)
	}
	writeStrings(h, policy.Modules)
	writeUint(h, uint64(len(policy.Imports)))
	for _, imp := range policy.Imports {
		writeString(h, imp.Alias)
		writeString(h, imp.Source.String())
	}
	writeUint(h, uint64(len(policy.Bindings)))
	for _, binding := range policy.Bindings {
		writeString(h, binding.Name)
		if err := writeBindingValue(h, binding.Value); err != nil {
			return [32]byte{}, err
		}
	}
	writeStrings(h, policy.AllowClasses)
	writeUint(h, uint64(len(policy.AllowCommands)))
	for _, command := range policy.AllowCommands {
		writeUint(h, uint64(command))
	}
	writeUint(h, uint64(len(policy.SendTargets)))
	for _, target := range policy.SendTargets {
		writeString(h, target.String())
	}
	h.Write(imports[:])
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// Binding value tags; integers and floats are distinct Lua values.
const (
	bindingTagNil byte = iota
	bindingTagFalse
	bindingTagTrue
	bindingTagInteger
	bindingTagFloat
	bindingTagString
	bindingTagList
	bindingTagMap
)

// writeBindingValue hashes a binding value as copyBindings produces it, with
// map keys in sorted order.
func writeBindingValue(h hash.Hash, v any) error {
	switch x := v.(type) {
	case nil:
		h.Write([]byte{bindingTagNil})
	case bool:
		if x {
			h.Write([]byte{bindingTagTrue})
		} else {
			h.Write([]byte{bindingTagFalse})
		}
	case int64:
		h.Write([]byte{bindingTagInteger})
		writeUint(h, uint64(x))
	case float64:
		h.Write([]byte{bindingTagFloat})
		writeUint(h, math.Float64bits(x))
	case string:
		h.Write([]byte{bindingTagString})
		writeString(h, x)
	case []any:
		h.Write([]byte{bindingTagList})
		writeUint(h, uint64(len(x)))
		for _, item := range x {
			if err := writeBindingValue(h, item); err != nil {
				return err
			}
		}
	case map[string]any:
		h.Write([]byte{bindingTagMap})
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		writeUint(h, uint64(len(keys)))
		for _, k := range keys {
			writeString(h, k)
			if err := writeBindingValue(h, x[k]); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("%w: unsupported %T value", apihost.ErrEvalBindingInvalid, v)
	}
	return nil
}

func writeUint(h hash.Hash, v uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	h.Write(buf[:])
}

func writeString(h hash.Hash, s string) {
	writeUint(h, uint64(len(s)))
	h.Write([]byte(s))
}

func writeStrings(h hash.Hash, ss []string) {
	writeUint(h, uint64(len(ss)))
	for _, s := range ss {
		writeString(h, s)
	}
}

func uniqueSortedStrings(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	dst := append([]string(nil), src...)
	sort.Strings(dst)
	n := 0
	for _, v := range dst {
		if n == 0 || dst[n-1] != v {
			dst[n] = v
			n++
		}
	}
	return dst[:n]
}

func uniqueSortedCommands(src []apihost.EvalCommand) []apihost.EvalCommand {
	if len(src) == 0 {
		return nil
	}
	dst := append([]apihost.EvalCommand(nil), src...)
	sort.Slice(dst, func(i, j int) bool { return dst[i] < dst[j] })
	n := 0
	for _, v := range dst {
		if n == 0 || dst[n-1] != v {
			dst[n] = v
			n++
		}
	}
	return dst[:n]
}

func uniqueSortedPIDs(src []pid.PID) []pid.PID {
	if len(src) == 0 {
		return nil
	}
	dst := append([]pid.PID(nil), src...)
	sort.Slice(dst, func(i, j int) bool { return dst[i].String() < dst[j].String() })
	n := 0
	for _, v := range dst {
		if n == 0 || dst[n-1].String() != v.String() {
			dst[n] = v
			n++
		}
	}
	return dst[:n]
}

func evalModuleClasses(extra []string) []string {
	return uniqueSortedStrings(append(append([]string(nil), evalBaseModuleClasses...), extra...))
}

func evalMethod(method string) string {
	if method == "" {
		return defaultEvalMethod
	}
	return method
}
