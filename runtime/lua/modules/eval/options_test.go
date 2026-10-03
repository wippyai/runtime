// SPDX-License-Identifier: MPL-2.0

package eval

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	apihost "github.com/wippyai/runtime/api/host"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
)

// evalLua runs src as a chunk and returns a state whose top value is the
// chunk result.
func evalLua(t *testing.T, src string) (*lua.LState, int) {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	require.NoError(t, l.DoString("return "+src))
	return l, l.GetTop()
}

func compile(t *testing.T, src string) (compileOptions, error) {
	l, idx := evalLua(t, src)
	return parseCompileOptions(l, idx)
}

func spawn(t *testing.T, src string) (spawnOptions, error) {
	l, idx := evalLua(t, src)
	return parseSpawnOptions(l, idx)
}

func TestOptions_Absent(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	c, err := parseCompileOptions(l, 1)
	require.NoError(t, err)
	assert.Equal(t, compileOptions{}, c)

	s, err := parseSpawnOptions(l, 1)
	require.NoError(t, err)
	assert.Equal(t, spawnOptions{}, s)

	c, err = compile(t, "nil")
	require.NoError(t, err)
	assert.Equal(t, compileOptions{}, c)

	s, err = spawn(t, "nil")
	require.NoError(t, err)
	assert.Equal(t, spawnOptions{}, s)
}

func TestOptions_EmptyTable(t *testing.T) {
	c, err := compile(t, "{}")
	require.NoError(t, err)
	assert.Equal(t, apihost.EvalSendObjectCapability, c.Policy.SendMode)

	s, err := spawn(t, "{}")
	require.NoError(t, err)
	assert.Equal(t, apihost.EvalLinkRequired, s.LinkMode)
	assert.Empty(t, s.Input)
}

func TestOptions_NotTable(t *testing.T) {
	for _, src := range []string{"1", `"x"`, "true", "function() end"} {
		_, err := compile(t, src)
		assert.EqualError(t, err, "eval options must be table", src)
		_, err = spawn(t, src)
		assert.EqualError(t, err, "eval options must be table", src)
	}
}

func TestOptions_UnknownKeys(t *testing.T) {
	const msg = "eval options contains unknown or non-string field"
	for _, src := range []string{
		`{bogus = 1}`,
		`{[1] = "x"}`,
		`{modules = {}, [true] = 1}`,
	} {
		_, err := compile(t, src)
		assert.EqualError(t, err, msg, src)
		_, err = spawn(t, src)
		assert.EqualError(t, err, msg, src)
	}

	for _, key := range []string{"name", "network", "input", "contents", "link", "detached", "monitor_only"} {
		_, err := compile(t, fmt.Sprintf("{%s = 1}", key))
		assert.EqualError(t, err, msg, "compile must reject spawn key "+key)
	}

	_, err := compile(t, `{policy = {method = "x"}}`)
	assert.EqualError(t, err, "eval policy contains unknown or non-string field")
	_, err = compile(t, `{policy = {[1] = "x"}}`)
	assert.EqualError(t, err, "eval policy contains unknown or non-string field")
	_, err = compile(t, `{policy = {policy = {}}}`)
	assert.EqualError(t, err, "eval policy contains unknown or non-string field")
	_, err = compile(t, `{limits = {bogus = 1}}`)
	assert.EqualError(t, err, "eval limits contains unknown or non-string field")
	_, err = compile(t, `{limits = {limits = {}}}`)
	assert.EqualError(t, err, "eval limits contains unknown or non-string field")
}

func TestOptions_StringFields(t *testing.T) {
	c, err := compile(t, `{method = "run"}`)
	require.NoError(t, err)
	assert.Equal(t, "run", c.Method)

	s, err := spawn(t, `{method = "run", name = "worker", network = "net"}`)
	require.NoError(t, err)
	assert.Equal(t, "run", s.Method)
	assert.Equal(t, "worker", s.Name)
	assert.Equal(t, "net", s.Network)

	_, err = compile(t, `{method = 1}`)
	assert.EqualError(t, err, "eval method must be string")
	_, err = spawn(t, `{name = 1}`)
	assert.EqualError(t, err, "eval name must be string")
	_, err = spawn(t, `{network = {}}`)
	assert.EqualError(t, err, "eval network must be string")
}

func TestOptions_CompileMode(t *testing.T) {
	tests := []struct {
		src  string
		mode apihost.EvalCompileMode
	}{
		{`"lite"`, apihost.EvalCompileLite},
		{`"LITE"`, apihost.EvalCompileLite},
		{`""`, apihost.EvalCompileLite},
		{`"typed"`, apihost.EvalCompileTyped},
	}
	for _, tt := range tests {
		c, err := compile(t, `{compile = `+tt.src+`}`)
		require.NoError(t, err, tt.src)
		assert.Equal(t, tt.mode, c.Policy.CompileMode, tt.src)
	}

	_, err := compile(t, `{compile = "fast"}`)
	assert.EqualError(t, err, `unknown eval compile mode "fast"`)
	c, err := compile(t, `{compile = "jit"}`)
	require.NoError(t, err)
	assert.Equal(t, apihost.EvalCompileJIT, c.Policy.CompileMode, "jit parses and admission reports it unsupported")
	_, err = compile(t, `{compile = 1}`)
	assert.EqualError(t, err, "eval compile mode must be string")
}

func TestOptions_SendMode(t *testing.T) {
	tests := []struct {
		src  string
		mode apihost.EvalSendMode
	}{
		{"cap", apihost.EvalSendObjectCapability},
		{"object_cap", apihost.EvalSendObjectCapability},
		{"object-capability", apihost.EvalSendObjectCapability},
		{"object_capability", apihost.EvalSendObjectCapability},
		{"", apihost.EvalSendObjectCapability},
		{"deny", apihost.EvalSendDenied},
		{"denied", apihost.EvalSendDenied},
		{"none", apihost.EvalSendDenied},
		{"explicit", apihost.EvalSendExplicitGrant},
		{"grant", apihost.EvalSendExplicitGrant},
		{"grants", apihost.EvalSendExplicitGrant},
		{"policy", apihost.EvalSendPolicy},
		{"DENY", apihost.EvalSendDenied},
	}
	for _, tt := range tests {
		c, err := compile(t, fmt.Sprintf(`{send = %q}`, tt.src))
		require.NoError(t, err, tt.src)
		assert.Equal(t, tt.mode, c.Policy.SendMode, tt.src)
	}

	_, err := compile(t, `{send = "all"}`)
	assert.EqualError(t, err, `unknown eval send mode "all"`)
	_, err = compile(t, `{send = true}`)
	assert.EqualError(t, err, "eval send mode must be string")
}

func TestOptions_LinkMode(t *testing.T) {
	tests := []struct {
		name string
		src  string
		err  string
		mode apihost.EvalLinkMode
	}{
		{"default", `{}`, "", apihost.EvalLinkRequired},
		{"required", `{link = "required"}`, "", apihost.EvalLinkRequired},
		{"empty", `{link = ""}`, "", apihost.EvalLinkRequired},
		{"detached", `{link = "detached"}`, "", apihost.EvalLinkDetached},
		{"monitor", `{link = "monitor"}`, "", apihost.EvalLinkMonitorOnly},
		{"monitor_only", `{link = "monitor_only"}`, "", apihost.EvalLinkMonitorOnly},
		{"monitor-only", `{link = "monitor-only"}`, "", apihost.EvalLinkMonitorOnly},
		{"upper", `{link = "DETACHED"}`, "", apihost.EvalLinkDetached},
		{"flag detached", `{detached = true}`, "", apihost.EvalLinkDetached},
		{"flag monitor_only", `{monitor_only = true}`, "", apihost.EvalLinkMonitorOnly},
		{"flag false ignored", `{detached = false, monitor_only = false}`, "", apihost.EvalLinkRequired},
		{"detached flag and link agree", `{detached = true, link = "detached"}`, "", apihost.EvalLinkDetached},
		{"monitor flag and link agree", `{monitor_only = true, link = "monitor"}`, "", apihost.EvalLinkMonitorOnly},
		{"false flag with link", `{detached = false, link = "monitor"}`, "", apihost.EvalLinkMonitorOnly},
		{"detached vs monitor_only", `{detached = true, monitor_only = true}`, "eval monitor_only conflicts with detached", 0},
		{"detached vs link monitor", `{detached = true, link = "monitor"}`, "eval link conflicts with detached", 0},
		{"monitor_only vs link detached", `{monitor_only = true, link = "detached"}`, "eval link conflicts with monitor_only", 0},
		{"detached vs link required", `{detached = true, link = "required"}`, "eval link conflicts with detached", 0},
		{"unknown link", `{link = "sideways"}`, `unknown eval link mode "sideways"`, 0},
		{"link type", `{link = 1}`, "eval link must be string", 0},
		{"detached type", `{detached = "yes"}`, "eval detached must be boolean", 0},
		{"monitor_only type", `{monitor_only = 1}`, "eval monitor_only must be boolean", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := spawn(t, tt.src)
			if tt.err != "" {
				assert.EqualError(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.mode, s.LinkMode)
		})
	}
}

func TestOptions_Modules(t *testing.T) {
	c, err := compile(t, `{modules = {"json", "time"}, allow_classes = {"io"}}`)
	require.NoError(t, err)
	assert.Equal(t, []string{"json", "time"}, c.Policy.Modules)
	assert.Equal(t, []string{"io"}, c.Policy.AllowClasses)

	c, err = compile(t, `{modules = {}}`)
	require.NoError(t, err)
	assert.NotNil(t, c.Policy.Modules)
	assert.Empty(t, c.Policy.Modules)
}

func TestOptions_StringArrayErrors(t *testing.T) {
	for _, key := range []string{"modules", "allow_classes"} {
		tests := []struct{ src, err string }{
			{`"x"`, "eval %s must be array"},
			{`5`, "eval %s must be array"},
			{`{"a", nil, "c"}`, "eval %s must be dense array"},
			{`{a = "x"}`, "eval %s must be dense array"},
			{`{"a", x = "b"}`, "eval %s must be dense array"},
			{`{[0] = "a"}`, "eval %s must be dense array"},
			{`{[2] = "a"}`, "eval %s must be dense array"},
			{`{"a", 1}`, "eval %s[2] must be string"},
			{`{{}}`, "eval %s[1] must be string"},
		}
		for _, tt := range tests {
			_, err := compile(t, fmt.Sprintf("{%s = %s}", key, tt.src))
			assert.EqualError(t, err, fmt.Sprintf(tt.err, key), key+" "+tt.src)
		}
	}
}

func TestOptions_Imports(t *testing.T) {
	c, err := compile(t, `{imports = {b = "app:b", a = "app.sub:a"}}`)
	require.NoError(t, err)
	assert.Equal(t, []apihost.EvalImport{
		{Alias: "a", Source: registry.ParseID("app.sub:a")},
		{Alias: "b", Source: registry.ParseID("app:b")},
	}, c.Policy.Imports)

	tests := []struct{ src, err string }{
		{`"x"`, "eval imports must be table"},
		{`{a = "nocolon"}`, "eval import a requires namespace:name source"},
		{`{a = ":name"}`, "eval import a requires namespace:name source"},
		{`{a = "ns:"}`, "eval import a requires namespace:name source"},
		{`{a = ""}`, "eval import a requires namespace:name source"},
		{`{a = {}}`, "eval imports entries must be alias=registry_id strings"},
		{`{a = 1}`, "eval imports entries must be alias=registry_id strings"},
		{`{"app:a"}`, "eval imports entries must be alias=registry_id strings"},
	}
	for _, tt := range tests {
		_, err := compile(t, `{imports = `+tt.src+`}`)
		assert.EqualError(t, err, tt.err, tt.src)
	}
}

func TestOptions_Bindings(t *testing.T) {
	c, err := compile(t, `{bindings = {
		n = 1.5, i = 3, s = "x", b = true,
		t = {1, 2, {k = "v"}},
		_ok9 = {},
	}}`)
	require.NoError(t, err)
	byName := map[string]any{}
	for _, b := range c.Policy.Bindings {
		byName[b.Name] = b.Value
	}
	require.Len(t, c.Policy.Bindings, 6)
	assert.Equal(t, "_ok9", c.Policy.Bindings[0].Name)
	assert.Equal(t, 1.5, byName["n"])
	assert.EqualValues(t, 3, byName["i"])
	assert.Equal(t, "x", byName["s"])
	assert.Equal(t, true, byName["b"])
	assert.Equal(t, []any{int64(1), int64(2), map[string]any{"k": "v"}}, normalizeInts(byName["t"]))

	_, err = compile(t, `{bindings = "x"}`)
	assert.EqualError(t, err, "eval bindings must be table")

	invalid := []string{
		`{f = function() end}`,
		`{t = {f = function() end}}`,
		`{t = {{print}}}`,
		`{u = coroutine.create(function() end)}`,
		`{["1x"] = 1}`,
		`{["a-b"] = 1}`,
		`{[""] = 1}`,
		`{["a b"] = 1}`,
		`{1}`,
		`{t = (function() local t = {} t.self = t return t end)()}`,
	}
	for _, src := range invalid {
		_, err := compile(t, `{bindings = `+src+`}`)
		require.Error(t, err, src)
		assert.Contains(t, err.Error(), "eval binding invalid", src)
	}
}

func normalizeInts(v any) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = normalizeInts(x[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = normalizeInts(e)
		}
		return out
	case float64:
		return int64(x)
	default:
		return v
	}
}

func TestOptions_Commands(t *testing.T) {
	tests := []struct {
		src  string
		want []apihost.EvalCommand
	}{
		{`{commands = {"spawn"}}`, []apihost.EvalCommand{apihost.EvalCommandSpawn}},
		{`{commands = {"upgrade"}}`, []apihost.EvalCommand{apihost.EvalCommandUpgrade}},
		{`{commands = {2}}`, []apihost.EvalCommand{apihost.EvalCommandSpawn}},
		{`{commands = {10}}`, []apihost.EvalCommand{apihost.EvalCommandUpgrade}},
		{`{commands = {"exec", "terminate"}}`, []apihost.EvalCommand{9, 3}},
		{`{commands = {"process.lookup"}}`, []apihost.EvalCommand{apihost.EvalCommandLookup}},
		{`{commands = {"process.spawn", "process.upgrade"}}`, []apihost.EvalCommand{apihost.EvalCommandSpawn, apihost.EvalCommandUpgrade}},
		{`{commands = {"SPAWN", "Process.Upgrade"}}`, []apihost.EvalCommand{apihost.EvalCommandSpawn, apihost.EvalCommandUpgrade}},
		{`{allow_commands = {"spawn"}}`, []apihost.EvalCommand{apihost.EvalCommandSpawn}},
		{`{commands = {"spawn"}, allow_commands = {"process.spawn"}}`, []apihost.EvalCommand{apihost.EvalCommandSpawn}},
		{`{commands = {"spawn", "upgrade"}, allow_commands = {"upgrade", "spawn", "spawn"}}`, []apihost.EvalCommand{apihost.EvalCommandSpawn, apihost.EvalCommandUpgrade}},
	}
	for _, tt := range tests {
		c, err := compile(t, tt.src)
		require.NoError(t, err, tt.src)
		assert.Equal(t, tt.want, c.Policy.AllowCommands, tt.src)
	}

	errs := []struct{ src, err string }{
		{`{commands = {"spawn"}, allow_commands = {"upgrade"}}`, "eval allow_commands conflicts with commands"},
		{`{commands = {"spawn"}, allow_commands = {}}`, "eval allow_commands conflicts with commands"},
		{`{commands = {"spawn", "bogus"}}`, `eval commands[2]: unknown command "bogus"`},
		{`{allow_commands = {"process.send"}}`, `eval allow_commands[1]: unknown command "process.send"`},
		{`{commands = {2.0}}`, "eval commands[1]: command must be string or integer"},
		{`{commands = {300}}`, "eval commands[1]: unknown command 300"},
		{`{commands = {1.5}}`, "eval commands[1]: command must be string or integer"},
		{`{commands = {true}}`, "eval commands[1]: command must be string or integer"},
		{`{commands = {{}}}`, "eval commands[1]: command must be string or integer"},
		{`{commands = "spawn"}`, "eval commands must be array"},
		{`{allow_commands = "spawn"}`, "eval allow_commands must be array"},
		{`{commands = {"spawn", nil, "upgrade"}}`, "eval commands must be dense array"},
	}
	for _, tt := range errs {
		_, err := compile(t, tt.src)
		assert.EqualError(t, err, tt.err, tt.src)
	}
}

func TestOptions_SendTargets(t *testing.T) {
	p1, err := pid.ParsePID("{h|u1}")
	require.NoError(t, err)
	p2, err := pid.ParsePID("{n@h|u2}")
	require.NoError(t, err)
	c, err := compile(t, fmt.Sprintf(`{send = "explicit", send_targets = {%q, %q}}`, p1.String(), p2.String()))
	require.NoError(t, err)
	assert.Equal(t, apihost.EvalSendExplicitGrant, c.Policy.SendMode)
	assert.Equal(t, []pid.PID{p1, p2}, c.Policy.SendTargets)

	tests := []struct{ src, err string }{
		{`"x"`, "eval send_targets must be array"},
		{`{"a", nil, "b"}`, "eval send_targets must be dense array"},
	}
	for _, tt := range tests {
		_, err := compile(t, `{send_targets = `+tt.src+`}`)
		assert.EqualError(t, err, tt.err, tt.src)
	}
	for _, src := range []string{`{"nobraces"}`, `{"{nopipe}"}`, `{1}`, `{{}}`} {
		_, err := compile(t, `{send_targets = `+src+`}`)
		require.Error(t, err, src)
		assert.Contains(t, err.Error(), "eval send_targets[1]: ", src)
		assert.Contains(t, err.Error(), "invalid pid format", src)
	}
}

func TestOptions_AllowDetached(t *testing.T) {
	c, err := compile(t, `{allow_detached = true}`)
	require.NoError(t, err)
	assert.True(t, c.Policy.AllowDetached)

	c, err = compile(t, `{allow_detached = false}`)
	require.NoError(t, err)
	assert.False(t, c.Policy.AllowDetached)

	_, err = compile(t, `{allow_detached = 1}`)
	assert.EqualError(t, err, "eval allow_detached must be boolean")
}

func TestOptions_Limits(t *testing.T) {
	flat, err := compile(t, `{
		memory_limit_bytes = 1000, heap_reserve_bytes = 2000, tick_budget = 3,
		max_steps = 4, mailbox_capacity = 5, max_children = 6,
	}`)
	require.NoError(t, err)
	want := apihost.EvalPolicy{
		MemoryLimitBytes: 1000, HeapReserveBytes: 2000, TickBudget: 3,
		MaxSteps: 4, MailboxCapacity: 5, MaxChildren: 6,
	}
	assert.Equal(t, want, flat.Policy)

	nested, err := compile(t, `{limits = {
		memory_limit_bytes = 1000, heap_reserve_bytes = 2000, tick_budget = 3,
		max_steps = 4, mailbox_capacity = 5, max_children = 6,
	}}`)
	require.NoError(t, err)
	assert.Equal(t, want, nested.Policy)

	inPolicy, err := compile(t, `{policy = {limits = {max_steps = 4}, tick_budget = 3}}`)
	require.NoError(t, err)
	assert.EqualValues(t, 4, inPolicy.Policy.MaxSteps)
	assert.EqualValues(t, 3, inPolicy.Policy.TickBudget)

	mixed, err := compile(t, `{memory_bytes = 7, limits = {max_steps = 9}}`)
	require.NoError(t, err)
	assert.EqualValues(t, 7, mixed.Policy.MemoryLimitBytes)
	assert.EqualValues(t, 9, mixed.Policy.MaxSteps)
}

func TestOptions_MemoryBytesAlias(t *testing.T) {
	c, err := compile(t, `{memory_bytes = 4096}`)
	require.NoError(t, err)
	assert.EqualValues(t, 4096, c.Policy.MemoryLimitBytes)

	c, err = compile(t, `{limits = {memory_bytes = 4096}}`)
	require.NoError(t, err)
	assert.EqualValues(t, 4096, c.Policy.MemoryLimitBytes)
}

func TestOptions_LimitConflicts(t *testing.T) {
	tests := []struct{ src, err string }{
		{`{memory_bytes = 1, memory_limit_bytes = 2}`, "eval memory_bytes conflicts with memory_limit_bytes"},
		{`{max_steps = 1, limits = {max_steps = 1}}`, "eval limits.max_steps conflicts with max_steps"},
		{`{tick_budget = 1, limits = {tick_budget = 2}}`, "eval limits.tick_budget conflicts with tick_budget"},
		{`{memory_limit_bytes = 1, limits = {memory_bytes = 1}}`, "eval limits.memory_bytes conflicts with memory_limit_bytes"},
		{`{memory_bytes = 1, limits = {memory_limit_bytes = 1}}`, "eval limits.memory_limit_bytes conflicts with memory_bytes"},
		{`{limits = {memory_bytes = 1, memory_limit_bytes = 1}}`, "eval limits.memory_bytes conflicts with limits.memory_limit_bytes"},
		{`{max_children = 1, limits = {max_children = 1}}`, "eval limits.max_children conflicts with max_children"},
		{`{policy = {max_steps = 1, limits = {max_steps = 2}}}`, "eval limits.max_steps conflicts with max_steps"},
	}
	for _, tt := range tests {
		_, err := compile(t, tt.src)
		assert.EqualError(t, err, tt.err, tt.src)
	}
}

func TestOptions_LimitRanges(t *testing.T) {
	tests := []struct {
		key string
		src string
		err string
	}{
		{"memory_limit_bytes", "-1", "eval memory_limit_bytes must be non-negative integer"},
		{"memory_bytes", "-1", "eval memory_bytes must be non-negative integer"},
		{"heap_reserve_bytes", "-1", "eval heap_reserve_bytes must be non-negative integer"},
		{"tick_budget", "-1", "eval tick_budget must be non-negative integer"},
		{"tick_budget", "2.0", "eval tick_budget must be non-negative integer"},
		{"max_steps", "3.0", "eval max_steps must be non-negative integer"},
		{"max_steps", "-1", "eval max_steps must be non-negative integer"},
		{"mailbox_capacity", "-1", "eval mailbox_capacity must be non-negative integer"},
		{"max_children", "-1", "eval max_children must be non-negative integer"},
		{"max_steps", "1.5", "eval max_steps must be non-negative integer"},
		{"max_steps", `"1"`, "eval max_steps must be non-negative integer"},
		{"max_steps", "true", "eval max_steps must be non-negative integer"},
		{"max_steps", "{}", "eval max_steps must be non-negative integer"},
		{"max_steps", "1e300", "eval max_steps must be non-negative integer"},
		{"tick_budget", "0", "eval tick_budget must be greater than zero"},
		{"max_steps", "0", "eval max_steps must be greater than zero"},
		{"mailbox_capacity", "0", "eval mailbox_capacity must be greater than zero"},
		{"tick_budget", "4294967296", "eval tick_budget exceeds uint32"},
		{"mailbox_capacity", "4294967296", "eval mailbox_capacity exceeds uint32"},
		{"max_children", "4294967296", "eval max_children exceeds uint32"},
	}
	for _, tt := range tests {
		_, err := compile(t, fmt.Sprintf("{%s = %s}", tt.key, tt.src))
		assert.EqualError(t, err, tt.err, tt.key+" "+tt.src)
		_, err = compile(t, fmt.Sprintf("{limits = {%s = %s}}", tt.key, tt.src))
		assert.EqualError(t, err, "eval limits."+tt.err[len("eval "):], "nested "+tt.key+" "+tt.src)
	}

	ok := []struct {
		key string
		src string
	}{
		{"memory_limit_bytes", "0"},
		{"memory_bytes", "0"},
		{"heap_reserve_bytes", "0"},
		{"max_children", "0"},
		{"max_children", "4294967295"},
		{"tick_budget", "4294967295"},
		{"mailbox_capacity", "4294967295"},
		{"max_steps", "4294967296"},
		{"memory_limit_bytes", "8589934592"},
	}
	for _, tt := range ok {
		_, err := compile(t, fmt.Sprintf("{%s = %s}", tt.key, tt.src))
		assert.NoError(t, err, tt.key+" "+tt.src)
	}

	c, err := compile(t, `{max_steps = 9007199254740992}`)
	require.NoError(t, err)
	assert.EqualValues(t, uint64(9007199254740992), c.Policy.MaxSteps)
}

func TestOptions_LimitsTableType(t *testing.T) {
	_, err := compile(t, `{limits = 5}`)
	assert.EqualError(t, err, "eval limits must be table")
	_, err = compile(t, `{limits = {[1] = 1}}`)
	assert.EqualError(t, err, "eval limits contains unknown or non-string field")
}

func TestOptions_NestedPolicy(t *testing.T) {
	c, err := compile(t, `{method = "m", policy = {
		compile = "typed", modules = {"json"}, send = "none",
		commands = {"spawn"}, allow_detached = true, max_steps = 10,
	}}`)
	require.NoError(t, err)
	assert.Equal(t, "m", c.Method)
	assert.Equal(t, apihost.EvalCompileTyped, c.Policy.CompileMode)
	assert.Equal(t, []string{"json"}, c.Policy.Modules)
	assert.Equal(t, apihost.EvalSendDenied, c.Policy.SendMode)
	assert.Equal(t, []apihost.EvalCommand{apihost.EvalCommandSpawn}, c.Policy.AllowCommands)
	assert.True(t, c.Policy.AllowDetached)
	assert.EqualValues(t, 10, c.Policy.MaxSteps)

	c, err = compile(t, `{policy = {}}`)
	require.NoError(t, err)

	flat, err := compile(t, `{compile = "typed", modules = {"json"}, send = "none", commands = {"spawn"}, allow_detached = true, max_steps = 10}`)
	require.NoError(t, err)
	nested, err := compile(t, `{policy = {compile = "typed", modules = {"json"}, send = "none", commands = {"spawn"}, allow_detached = true, max_steps = 10}}`)
	require.NoError(t, err)
	assert.Equal(t, flat.Policy, nested.Policy)

	_, err = compile(t, `{policy = "x"}`)
	assert.EqualError(t, err, "eval policy must be table")
	_, err = compile(t, `{policy = true}`)
	assert.EqualError(t, err, "eval policy must be table")

	s, err := spawn(t, `{policy = {modules = {"json"}}, name = "n", link = "monitor"}`)
	require.NoError(t, err)
	assert.Equal(t, []string{"json"}, s.Policy.Modules)
	assert.Equal(t, "n", s.Name)
	assert.Equal(t, apihost.EvalLinkMonitorOnly, s.LinkMode)
}

func TestOptions_NestedPolicyConflictsWithTopLevel(t *testing.T) {
	fields := map[string]string{
		"compile":            `"typed"`,
		"modules":            `{}`,
		"imports":            `{}`,
		"bindings":           `{}`,
		"allow_classes":      `{}`,
		"commands":           `{}`,
		"allow_commands":     `{}`,
		"send":               `"none"`,
		"send_targets":       `{}`,
		"memory_limit_bytes": `1000`,
		"memory_bytes":       `1000`,
		"heap_reserve_bytes": `1000`,
		"tick_budget":        `1`,
		"max_steps":          `1`,
		"mailbox_capacity":   `1`,
		"max_children":       `1`,
		"limits":             `{}`,
		"allow_detached":     `true`,
	}
	for key, val := range fields {
		src := fmt.Sprintf(`{policy = {modules = {}}, %s = %s}`, key, val)
		_, err := compile(t, src)
		assert.EqualError(t, err, fmt.Sprintf("eval policy table conflicts with top-level policy field %q", key), key)
		_, err = spawn(t, src)
		assert.EqualError(t, err, fmt.Sprintf("eval policy table conflicts with top-level policy field %q", key), key)
	}

	_, err := compile(t, `{policy = {}, method = "m"}`)
	assert.NoError(t, err, "method is not a policy field")
	_, err = spawn(t, `{policy = {}, name = "n", link = "detached", input = 1}`)
	assert.NoError(t, err, "spawn fields are not policy fields")
}

func TestOptions_SpawnInput(t *testing.T) {
	s, err := spawn(t, `{input = {a = 1}}`)
	require.NoError(t, err)
	require.Len(t, s.Input, 1)

	s, err = spawn(t, `{input = "first", contents = {"second", 3}}`)
	require.NoError(t, err)
	require.Len(t, s.Input, 3)
	assert.Equal(t, lua.LString("first"), s.Input[0].Data())
	assert.Equal(t, lua.LString("second"), s.Input[1].Data())

	s, err = spawn(t, `{contents = {"only"}}`)
	require.NoError(t, err)
	require.Len(t, s.Input, 1)
	assert.Equal(t, lua.LString("only"), s.Input[0].Data())

	s, err = spawn(t, `{contents = {}}`)
	require.NoError(t, err)
	assert.Empty(t, s.Input)

	tests := []struct{ src, err string }{
		{`{contents = "x"}`, "eval contents must be array"},
		{`{contents = {"a", nil, "c"}}`, "eval contents must be dense array"},
		{`{contents = {k = "v"}}`, "eval contents must be dense array"},
	}
	for _, tt := range tests {
		_, err := spawn(t, tt.src)
		assert.EqualError(t, err, tt.err, tt.src)
	}
}

func TestOptions_ErrorsLeaveZeroValue(t *testing.T) {
	c, err := compile(t, `{modules = {"json"}, max_steps = -1}`)
	require.Error(t, err)
	assert.Equal(t, compileOptions{}, c)

	s, err := spawn(t, `{name = "n", link = "bogus"}`)
	require.Error(t, err)
	assert.Equal(t, spawnOptions{}, s)
}

func TestOptions_BindingsAreBoundedTrees(t *testing.T) {
	c, err := compile(t, `{bindings = {cfg = {name = "x", list = {1, 2, 3}, nested = {deep = true}}, n = 7}}`)
	require.NoError(t, err)
	byName := map[string]any{}
	for _, b := range c.Policy.Bindings {
		byName[b.Name] = b.Value
	}
	assert.Equal(t, int64(7), byName["n"])
	cfg := byName["cfg"].(map[string]any)
	assert.Equal(t, []any{int64(1), int64(2), int64(3)}, cfg["list"])
	assert.Equal(t, map[string]any{"deep": true}, cfg["nested"])

	_, err = compile(t, `(function() local t = {}; local s = {t, t}; return {bindings = {x = s}} end)()`)
	assert.ErrorContains(t, err, "repeats a table", "shared subtables are rejected")

	// {t, t} graphs would cost 2^levels visits if shared tables were re-walked.
	_, err = compile(t, `(function() local t = {} for i = 1, 20 do t = {t, t} end return {bindings = {x = t}} end)()`)
	assert.ErrorContains(t, err, "repeats a table")
	_, err = compile(t, `(function() local t = {} for i = 1, 35 do t = {t, t} end return {bindings = {x = t}} end)()`)
	assert.ErrorContains(t, err, "eval binding invalid")

	_, err = compile(t, `{bindings = {x = {[1] = "a", tag = "b"}}}`)
	assert.ErrorContains(t, err, "mixes array and string keys", "mixed tables would lose keys")

	_, err = compile(t, `{bindings = {x = {[2] = "a"}}}`)
	assert.ErrorContains(t, err, "eval binding invalid")

	_, err = compile(t, `(function() local t = {} for i = 1, 40 do t = {t} end return {bindings = {x = t}} end)()`)
	assert.ErrorContains(t, err, "nests deeper than")

	_, err = compile(t, `(function() local t = {} for i = 1, 5000 do t[i] = {} end return {bindings = {x = t}} end)()`)
	assert.ErrorContains(t, err, "exceed")
}
