// SPDX-License-Identifier: MPL-2.0

package eval

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	lua "github.com/wippyai/go-lua"
	apihost "github.com/wippyai/runtime/api/host"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
)

// tableKey returns the string form of a string table key.
func tableKey(k lua.LValue) (string, bool) {
	s, ok := k.(lua.LString)
	return string(s), ok
}

// integerValue reads an integral Lua number as int64.
func integerValue(v lua.LValue) (int64, bool) {
	switch n := v.(type) {
	case lua.LInteger:
		return int64(n), true
	case lua.LNumber:
		f := float64(n)
		if f != math.Trunc(f) || f < -(1<<63) || f >= 1<<63 {
			return 0, false
		}
		return int64(f), true
	default:
		return 0, false
	}
}

// checkClosedKeys rejects non-string keys and keys outside allowed.
func checkClosedKeys(tbl *lua.LTable, allowed map[string]struct{}, name string) error {
	var bad bool
	tbl.ForEach(func(k, _ lua.LValue) {
		key, ok := tableKey(k)
		if !ok {
			bad = true
			return
		}
		if _, known := allowed[key]; !known {
			bad = true
		}
	})
	if bad {
		return fmt.Errorf("eval %s contains unknown or non-string field", name)
	}
	return nil
}

// denseArray returns the elements of a table holding exactly the keys 1..n.
func denseArray(tbl *lua.LTable) ([]lua.LValue, bool) {
	var max, count int64
	ok := true
	tbl.ForEach(func(k, _ lua.LValue) {
		n, isInt := integerValue(k)
		if !isInt || n <= 0 || n > math.MaxInt32 {
			ok = false
			return
		}
		if n > max {
			max = n
		}
		count++
	})
	if !ok || max != count {
		return nil, false
	}
	out := make([]lua.LValue, 0, max)
	for i := 1; i <= int(max); i++ {
		out = append(out, tbl.RawGetInt(i))
	}
	return out, true
}

func arrayField(raw lua.LValue, name string) ([]lua.LValue, error) {
	tbl, ok := raw.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("eval %s must be array", name)
	}
	items, ok := denseArray(tbl)
	if !ok {
		return nil, fmt.Errorf("eval %s must be dense array", name)
	}
	return items, nil
}

func stringArrayValue(raw lua.LValue, name string) ([]string, error) {
	if raw == lua.LNil {
		return nil, nil
	}
	items, err := arrayField(raw, name)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		s, ok := item.(lua.LString)
		if !ok {
			return nil, fmt.Errorf("eval %s[%d] must be string", name, i+1)
		}
		out = append(out, string(s))
	}
	return out, nil
}

func commandArrayValue(raw lua.LValue, name string) ([]apihost.EvalCommand, error) {
	if raw == lua.LNil {
		return nil, nil
	}
	items, err := arrayField(raw, name)
	if err != nil {
		return nil, err
	}
	out := make([]apihost.EvalCommand, 0, len(items))
	for i, item := range items {
		cmd, err := commandFromValue(item)
		if err != nil {
			return nil, fmt.Errorf("eval %s[%d]: %w", name, i+1, err)
		}
		out = append(out, cmd)
	}
	return out, nil
}

// commandAliasFields merges commands and allow_commands, which name the same
// policy field.
func commandAliasFields(opts *lua.LTable) ([]apihost.EvalCommand, error) {
	rawCommands := opts.RawGetString(evalOptionCommands)
	rawAllow := opts.RawGetString(evalOptionAllowCommands)
	commands, err := commandArrayValue(rawCommands, evalOptionCommands)
	if err != nil {
		return nil, err
	}
	allow, err := commandArrayValue(rawAllow, evalOptionAllowCommands)
	if err != nil {
		return nil, err
	}
	switch {
	case rawCommands == lua.LNil:
		return allow, nil
	case rawAllow == lua.LNil:
		return commands, nil
	case !commandSetEqual(commands, allow):
		return nil, errors.New("eval allow_commands conflicts with commands")
	default:
		return commands, nil
	}
}

func commandSetEqual(a, b []apihost.EvalCommand) bool {
	ua, ub := uniqueSortedCommands(a), uniqueSortedCommands(b)
	if len(ua) != len(ub) {
		return false
	}
	for i := range ua {
		if ua[i] != ub[i] {
			return false
		}
	}
	return true
}

func uniqueSortedCommands(src []apihost.EvalCommand) []apihost.EvalCommand {
	dst := append([]apihost.EvalCommand(nil), src...)
	sort.Slice(dst, func(i, j int) bool { return dst[i] < dst[j] })
	n := 0
	for _, c := range dst {
		if n == 0 || dst[n-1] != c {
			dst[n] = c
			n++
		}
	}
	return dst[:n]
}

func commandFromValue(v lua.LValue) (apihost.EvalCommand, error) {
	if n, ok := integerValue(v); ok {
		return 0, fmt.Errorf("unknown command %d", n)
	}
	s, ok := v.(lua.LString)
	if !ok {
		return 0, errors.New("command must be string or integer")
	}
	name := strings.TrimPrefix(strings.ToLower(string(s)), "process.")
	switch name {
	case "spawn":
		return apihost.EvalCommandSpawn, nil
	case "upgrade":
		return apihost.EvalCommandUpgrade, nil
	default:
		return 0, fmt.Errorf("unknown command %q", string(s))
	}
}

func importsValue(raw lua.LValue, name string) ([]apihost.EvalImport, error) {
	if raw == lua.LNil {
		return nil, nil
	}
	tbl, ok := raw.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("eval %s must be table", name)
	}
	var out []apihost.EvalImport
	var err error
	tbl.ForEach(func(k, v lua.LValue) {
		if err != nil {
			return
		}
		alias, kok := k.(lua.LString)
		src, vok := v.(lua.LString)
		if !kok || !vok {
			err = fmt.Errorf("eval %s entries must be alias=registry_id strings", name)
			return
		}
		id := registry.ParseID(string(src))
		if id.NS == "" || id.Name == "" {
			err = fmt.Errorf("eval import %s requires namespace:name source", string(alias))
			return
		}
		out = append(out, apihost.EvalImport{Alias: string(alias), Source: id})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return out, nil
}

func bindingsValue(raw lua.LValue, name string) ([]apihost.EvalBinding, error) {
	if raw == lua.LNil {
		return nil, nil
	}
	tbl, ok := raw.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("eval %s must be table", name)
	}
	var out []apihost.EvalBinding
	var err error
	conv := bindingConverter{seen: make(map[*lua.LTable]struct{})}
	tbl.ForEach(func(k, v lua.LValue) {
		if err != nil {
			return
		}
		key, ok := tableKey(k)
		if !ok {
			err = errors.New("eval binding invalid: name must be string")
			return
		}
		if !validBindingName(key) {
			err = fmt.Errorf("eval binding invalid: %q is not a valid identifier", key)
			return
		}
		var data any
		if data, err = conv.value(v, key, 0); err != nil {
			return
		}
		out = append(out, apihost.EvalBinding{Name: key, Value: data})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func validBindingName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z':
		case i > 0 && c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// bindingConverter copies binding data out of Lua in one bounded pass. A
// binding is a tree of nil, booleans, numbers, strings, arrays (keys 1..n)
// and string-keyed maps; every table is visited once, so repeated tables are
// rejected rather than re-walked.
type bindingConverter struct {
	seen  map[*lua.LTable]struct{}
	nodes int
}

func (c *bindingConverter) value(v lua.LValue, path string, depth int) (any, error) {
	switch x := v.(type) {
	case *lua.LNilType:
		return nil, nil
	case lua.LBool:
		return bool(x), nil
	case lua.LInteger:
		return int64(x), nil
	case lua.LNumber:
		return float64(x), nil
	case lua.LString:
		return string(x), nil
	case *lua.LTable:
		return c.table(x, path, depth)
	default:
		return nil, fmt.Errorf("eval binding invalid: %s has unsupported %s value", path, v.Type())
	}
}

func (c *bindingConverter) table(t *lua.LTable, path string, depth int) (any, error) {
	if _, dup := c.seen[t]; dup {
		return nil, fmt.Errorf("eval binding invalid: %s repeats a table", path)
	}
	c.seen[t] = struct{}{}
	if depth >= evalhost.MaxBindingDepth {
		return nil, fmt.Errorf("eval binding invalid: %s nests deeper than %d", path, evalhost.MaxBindingDepth)
	}
	c.nodes++
	if c.nodes > evalhost.MaxBindingNodes {
		return nil, fmt.Errorf("eval binding invalid: bindings exceed %d tables", evalhost.MaxBindingNodes)
	}

	n := t.Len()
	var arr []any
	var obj map[string]any
	count := 0
	var err error
	t.ForEach(func(k, item lua.LValue) {
		if err != nil {
			return
		}
		count++
		switch kk := k.(type) {
		case lua.LString:
			if obj == nil {
				obj = make(map[string]any)
			}
			var data any
			if data, err = c.value(item, path+"."+string(kk), depth+1); err == nil {
				obj[string(kk)] = data
			}
		case lua.LNumber, lua.LInteger:
			idx := int(lua.LVAsNumber(kk))
			if lua.LVAsNumber(kk) != lua.LNumber(idx) || idx < 1 || idx > n {
				err = fmt.Errorf("eval binding invalid: %s has non-sequence key %v", path, kk)
				return
			}
			if arr == nil {
				arr = make([]any, n)
			}
			var data any
			if data, err = c.value(item, fmt.Sprintf("%s[%d]", path, idx), depth+1); err == nil {
				arr[idx-1] = data
			}
		default:
			err = fmt.Errorf("eval binding invalid: %s has unsupported key type %s", path, k.Type())
		}
	})
	switch {
	case err != nil:
		return nil, err
	case arr != nil && obj != nil:
		return nil, fmt.Errorf("eval binding invalid: %s mixes array and string keys", path)
	case arr != nil:
		if count != n {
			return nil, fmt.Errorf("eval binding invalid: %s is not a dense array", path)
		}
		return arr, nil
	case obj != nil:
		return obj, nil
	default:
		return map[string]any{}, nil
	}
}

func pidArrayValue(raw lua.LValue, name string) ([]pid.PID, error) {
	if raw == lua.LNil {
		return nil, nil
	}
	items, err := arrayField(raw, name)
	if err != nil {
		return nil, err
	}
	out := make([]pid.PID, 0, len(items))
	for i, item := range items {
		s, ok := item.(lua.LString)
		if !ok {
			return nil, fmt.Errorf("eval %s[%d]: %w", name, i+1, pid.ErrInvalidPIDFormat)
		}
		p, err := pid.ParsePID(string(s))
		if err != nil {
			return nil, fmt.Errorf("eval %s[%d]: %w", name, i+1, err)
		}
		out = append(out, p)
	}
	return out, nil
}

func boolValue(raw lua.LValue, name string) (value bool, set bool, err error) {
	if raw == lua.LNil {
		return false, false, nil
	}
	b, ok := raw.(lua.LBool)
	if !ok {
		return false, false, fmt.Errorf("eval %s must be boolean", name)
	}
	return bool(b), true, nil
}

func optionalStringValue(raw lua.LValue, name string) (string, error) {
	if raw == lua.LNil {
		return "", nil
	}
	s, ok := raw.(lua.LString)
	if !ok {
		return "", fmt.Errorf("eval %s must be string", name)
	}
	return string(s), nil
}

func parseCompileMode(raw lua.LValue) (apihost.EvalCompileMode, error) {
	if raw == lua.LNil {
		return apihost.EvalCompileLite, nil
	}
	s, ok := raw.(lua.LString)
	if !ok {
		return apihost.EvalCompileLite, errors.New("eval compile mode must be string")
	}
	switch strings.ToLower(string(s)) {
	case "", "lite":
		return apihost.EvalCompileLite, nil
	case "typed":
		return apihost.EvalCompileTyped, nil
	default:
		return apihost.EvalCompileLite, fmt.Errorf("unknown eval compile mode %q", string(s))
	}
}

func parseSendMode(raw lua.LValue) (apihost.EvalSendMode, error) {
	if raw == lua.LNil {
		return apihost.EvalSendObjectCapability, nil
	}
	s, ok := raw.(lua.LString)
	if !ok {
		return apihost.EvalSendObjectCapability, errors.New("eval send mode must be string")
	}
	switch strings.ToLower(string(s)) {
	case "", "cap", "object_cap", "object-capability", "object_capability":
		return apihost.EvalSendObjectCapability, nil
	case "deny", "denied", "none":
		return apihost.EvalSendDenied, nil
	case "explicit", "grant", "grants":
		return apihost.EvalSendExplicitGrant, nil
	case "policy":
		return apihost.EvalSendPolicy, nil
	default:
		return apihost.EvalSendObjectCapability, fmt.Errorf("unknown eval send mode %q", string(s))
	}
}
