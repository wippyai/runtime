// SPDX-License-Identifier: MPL-2.0

package eval

import (
	"errors"
	"fmt"

	lua "github.com/wippyai/go-lua"
	apihost "github.com/wippyai/runtime/api/host"
	"github.com/wippyai/runtime/api/payload"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
)

const (
	evalOptionCompile       = "compile"
	evalOptionModules       = "modules"
	evalOptionImports       = "imports"
	evalOptionBindings      = "bindings"
	evalOptionAllowClasses  = "allow_classes"
	evalOptionCommands      = "commands"
	evalOptionAllowCommands = "allow_commands"
	evalOptionSend          = "send"
	evalOptionSendTargets   = "send_targets"
	evalOptionLimits        = "limits"
	evalOptionAllowDetached = "allow_detached"

	evalOptionMethod      = "method"
	evalOptionPolicy      = "policy"
	evalOptionName        = "name"
	evalOptionNetwork     = "network"
	evalOptionInput       = "input"
	evalOptionContents    = "contents"
	evalOptionLink        = "link"
	evalOptionDetached    = "detached"
	evalOptionMonitorOnly = "monitor_only"
)

// policyFieldKeys are the policy keys that precede the limit keys.
var policyFieldKeys = [...]string{
	evalOptionCompile,
	evalOptionModules,
	evalOptionImports,
	evalOptionBindings,
	evalOptionAllowClasses,
	evalOptionCommands,
	evalOptionAllowCommands,
	evalOptionSend,
	evalOptionSendTargets,
}

// policyTailKeys are the policy keys that follow the limit keys.
var policyTailKeys = [...]string{
	evalOptionLimits,
	evalOptionAllowDetached,
}

var (
	policyKeys  = newKeySet(allPolicyKeys()...)
	limitsKeys  = newKeySet(limitKeys()...)
	compileKeys = newKeySet(append(allPolicyKeys(), evalOptionMethod, evalOptionPolicy)...)
	spawnKeys   = newKeySet(append(allPolicyKeys(),
		evalOptionMethod, evalOptionPolicy, evalOptionName, evalOptionNetwork,
		evalOptionInput, evalOptionContents, evalOptionLink, evalOptionDetached,
		evalOptionMonitorOnly)...)
)

func allPolicyKeys() []string {
	keys := append([]string(nil), policyFieldKeys[:]...)
	keys = append(keys, limitKeys()...)
	return append(keys, policyTailKeys[:]...)
}

func limitKeys() []string {
	keys := make([]string, 0, len(limitSpecs))
	for _, spec := range limitSpecs {
		keys = append(keys, spec.name)
	}
	return keys
}

func newKeySet(keys ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		set[k] = struct{}{}
	}
	return set
}

type compileOptions struct {
	Method    string
	Policy    apihost.EvalPolicy
	PolicySet bool
}

type spawnOptions struct {
	Method    string
	Name      string
	Network   string
	Input     payload.Payloads
	Policy    apihost.EvalPolicy
	PolicySet bool
	LinkMode  apihost.EvalLinkMode
}

// optionsTable returns the options table at idx, or nil when absent.
func optionsTable(l *lua.LState, idx int) (*lua.LTable, error) {
	raw := l.Get(idx)
	if raw == lua.LNil {
		return nil, nil
	}
	tbl, ok := raw.(*lua.LTable)
	if !ok {
		return nil, errors.New("eval options must be table")
	}
	return tbl, nil
}

func parseCompileOptions(l *lua.LState, idx int) (compileOptions, error) {
	var out compileOptions
	opts, err := optionsTable(l, idx)
	if err != nil || opts == nil {
		return out, err
	}
	if err := checkClosedKeys(opts, compileKeys, "options"); err != nil {
		return out, err
	}
	if out.Policy, out.PolicySet, err = parsePolicyFromOptions(opts); err != nil {
		return compileOptions{}, err
	}
	if out.Method, err = optionalStringValue(opts.RawGetString(evalOptionMethod), evalOptionMethod); err != nil {
		return compileOptions{}, err
	}
	return out, nil
}

func parseSpawnOptions(l *lua.LState, idx int) (spawnOptions, error) {
	var out spawnOptions
	opts, err := optionsTable(l, idx)
	if err != nil || opts == nil {
		return out, err
	}
	if err := checkClosedKeys(opts, spawnKeys, "options"); err != nil {
		return out, err
	}
	if out.Policy, out.PolicySet, err = parsePolicyFromOptions(opts); err != nil {
		return spawnOptions{}, err
	}
	if out.Method, err = optionalStringValue(opts.RawGetString(evalOptionMethod), evalOptionMethod); err != nil {
		return spawnOptions{}, err
	}
	if out.LinkMode, err = parseLinkMode(opts); err != nil {
		return spawnOptions{}, err
	}
	if out.Name, err = optionalStringValue(opts.RawGetString(evalOptionName), evalOptionName); err != nil {
		return spawnOptions{}, err
	}
	if out.Network, err = optionalStringValue(opts.RawGetString(evalOptionNetwork), evalOptionNetwork); err != nil {
		return spawnOptions{}, err
	}
	if out.Input, err = parseInput(opts); err != nil {
		return spawnOptions{}, err
	}
	return out, nil
}

// parseInput exports input as the first payload and contents as the payloads
// that follow it.
func parseInput(opts *lua.LTable) (payload.Payloads, error) {
	var out payload.Payloads
	if input := opts.RawGetString(evalOptionInput); input != lua.LNil {
		out = append(out, luaconv.ExportPayload(input))
	}
	if raw := opts.RawGetString(evalOptionContents); raw != lua.LNil {
		items, err := arrayField(raw, "contents")
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			out = append(out, luaconv.ExportPayload(item))
		}
	}
	return out, nil
}

// parsePolicyFromOptions reads the policy from either the nested policy table
// or the top-level policy fields. The two forms do not mix.
func parsePolicyFromOptions(opts *lua.LTable) (apihost.EvalPolicy, bool, error) {
	raw := opts.RawGetString(evalOptionPolicy)
	if raw == lua.LNil {
		policy, err := parsePolicyFields(opts)
		return policy, hasPolicyField(opts) != "", err
	}
	nested, ok := raw.(*lua.LTable)
	if !ok {
		return apihost.EvalPolicy{}, false, errors.New("eval policy must be table")
	}
	if err := checkClosedKeys(nested, policyKeys, evalOptionPolicy); err != nil {
		return apihost.EvalPolicy{}, false, err
	}
	if field := hasPolicyField(opts); field != "" {
		return apihost.EvalPolicy{}, false, fmt.Errorf("eval policy table conflicts with top-level policy field %q", field)
	}
	policy, err := parsePolicyFields(nested)
	return policy, true, err
}

// hasPolicyField returns the first policy key present in opts.
func hasPolicyField(opts *lua.LTable) string {
	for _, key := range allPolicyKeys() {
		if opts.RawGetString(key) != lua.LNil {
			return key
		}
	}
	return ""
}

func parsePolicyFields(tbl *lua.LTable) (apihost.EvalPolicy, error) {
	policy := apihost.EvalPolicy{SendMode: apihost.EvalSendObjectCapability}
	var err error
	if policy.CompileMode, err = parseCompileMode(tbl.RawGetString(evalOptionCompile)); err != nil {
		return policy, err
	}
	if policy.Modules, err = stringArrayValue(tbl.RawGetString(evalOptionModules), evalOptionModules); err != nil {
		return policy, err
	}
	if policy.Imports, err = importsValue(tbl.RawGetString(evalOptionImports), evalOptionImports); err != nil {
		return policy, err
	}
	if policy.Bindings, err = bindingsValue(tbl.RawGetString(evalOptionBindings), evalOptionBindings); err != nil {
		return policy, err
	}
	if policy.AllowClasses, err = stringArrayValue(tbl.RawGetString(evalOptionAllowClasses), evalOptionAllowClasses); err != nil {
		return policy, err
	}
	if policy.AllowCommands, err = commandAliasFields(tbl); err != nil {
		return policy, err
	}
	if policy.SendMode, err = parseSendMode(tbl.RawGetString(evalOptionSend)); err != nil {
		return policy, err
	}
	if policy.SendTargets, err = pidArrayValue(tbl.RawGetString(evalOptionSendTargets), evalOptionSendTargets); err != nil {
		return policy, err
	}
	var limits limitSet
	if err := parseLimitFields(tbl, false, &limits); err != nil {
		return policy, err
	}
	if raw := tbl.RawGetString(evalOptionLimits); raw != lua.LNil {
		nested, ok := raw.(*lua.LTable)
		if !ok {
			return policy, fmt.Errorf("eval %s must be table", evalOptionLimits)
		}
		if err := checkClosedKeys(nested, limitsKeys, evalOptionLimits); err != nil {
			return policy, err
		}
		if err := parseLimitFields(nested, true, &limits); err != nil {
			return policy, err
		}
	}
	limits.apply(&policy)
	if allow, ok, err := boolValue(tbl.RawGetString(evalOptionAllowDetached), evalOptionAllowDetached); err != nil {
		return policy, err
	} else if ok {
		policy.AllowDetached = allow
	}
	return policy, nil
}
