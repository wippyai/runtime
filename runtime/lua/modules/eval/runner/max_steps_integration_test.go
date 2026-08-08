package runner

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
)

// The eval host counts scheduler steps, and a step is consumed per resume — so
// step pressure comes from yields (module calls that leave the VM), not from
// Lua-side loops. These tests drive real yields through the http client and pin
// both directions of the max_steps option: a limit below the yield count is
// refused, and a sufficient one completes. This is the end-to-end
// proof that the option parses in runner.run, travels the RunYield -> RunCmd
// path, and is enforced by the host loop.
func TestRunner_MaxSteps_Enforced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer server.Close()

	sched := newTestScheduler(evalhost.WithDefaultMaxSteps(5))
	sched.Start()
	defer sched.Stop()

	build := func(maxSteps string) string {
		return `
			local runner = require("eval_runner")

			local result, err = runner.run({
				source = [[
					local http = require("http_client")
					local function run(url)
						for i = 1, 20 do
							local _, herr = http.get(url)
							if herr then return nil, herr end
						end
						return "done"
					end
					return { run = run }
				]],
				method = "run",
				args = { "` + server.URL + `" },
				modules = { "http_client" },
				allow_classes = { "network", "io" },
				` + maxSteps + `
			})

			if err then
				return "ERR: " .. tostring(err)
			end
			return result
		`
	}

	run := func(t *testing.T, script string) string {
		ctx := sched.Context()
		proc := newLuaProcess(t, script)
		result, err := sched.Execute(ctx, uniqueTestPID(), proc, "", nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		if result.Error != nil {
			t.Fatalf("process error: %v", result.Error)
		}
		require.NotNil(t, result.Value)
		luaData := result.Value.Data()
		goData := value.ToGoAny(luaData.(lua.LValue))
		s, _ := goData.(string)
		return s
	}

	t.Run("limit below the yield count is refused", func(t *testing.T) {
		out := run(t, build("limits = { max_steps = 5 },"))
		require.Contains(t, out, "maximum step limit",
			"twenty yields against a five-step limit must exceed it, got %q", out)
	})

	t.Run("limit above the yield count completes", func(t *testing.T) {
		out := run(t, build("limits = { max_steps = 1000 },"))
		require.Contains(t, out, "done", "got %q", out)
	})

	t.Run("omitted limit inherits the host default", func(t *testing.T) {
		out := run(t, build(""))
		require.Contains(t, out, "maximum step limit",
			"an omitted limit must inherit the five-step host default, got %q", out)
	})

	t.Run("zero limit is unlimited", func(t *testing.T) {
		out := run(t, build("limits = { max_steps = 0 },"))
		require.Contains(t, out, "done", "got %q", out)
	})

	for _, tc := range []struct {
		name  string
		value string
	}{
		{name: "negative", value: "-1"},
		{name: "fractional", value: "1.5"},
		{name: "string", value: `"10"`},
	} {
		t.Run("invalid "+tc.name+" limit is rejected", func(t *testing.T) {
			out := run(t, build("limits = { max_steps = "+tc.value+" },"))
			require.Contains(t, out, "limits.max_steps must be a non-negative integer", "got %q", out)
		})
	}

	t.Run("non-table limits are rejected", func(t *testing.T) {
		out := run(t, build("limits = 5,"))
		require.Contains(t, out, "limits must be a table", "got %q", out)
	})

	t.Run("unknown limits are rejected", func(t *testing.T) {
		out := run(t, build("limits = { surprise = 5 },"))
		require.Contains(t, out, "limits contains unknown or non-string field", "got %q", out)
	})
}
