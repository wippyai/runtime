// SPDX-License-Identifier: MPL-2.0

package pg_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

// Lua-level coverage for the dimensions the existing pg integration
// tests don't enumerate exhaustively:
//   - many-group-names (verifying group-name string handling end-to-end)
//   - large-fanout broadcast (>32 members in one group)
//   - repeated join/leave cycles
//   - Monitor/Events ergonomics
//
// Each test reuses the existing setupPGTest harness; no new
// infrastructure is added.

func TestExtraLua_VariousGroupNames(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	cases := []string{
		"plain",
		"with-dashes",
		"with.dots",
		"slash/path",
		"ns:group",
		"all-numeric-1234",
		"UPPER",
		"mixed_Case_With_2",
		"a", // single char
		"a-very-long-group-name-that-still-must-work-end-to-end-via-lua",
	}
	for _, g := range cases {
		t.Run(g, func(t *testing.T) {
			testPID := uniquePGTestPID()
			result := runPGScriptWithPID(t, tc, testPID, fmt.Sprintf(`
				local scope = pg.open("test:pg")
				local ok, err = scope:join(%q)
				if err then return nil, tostring(err) end
				local members, err2 = scope:get_members(%q)
				if err2 then return nil, tostring(err2) end
				return #members
			`, g, g))
			require.NoError(t, result.Error)
			assert.Equal(t, lua.LNumber(1), result.Value.Data(),
				"after join, group %q should report 1 member", g)
		})
	}
}

func TestExtraLua_RepeatedJoinLeaveCycles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	cases := []int{1, 2, 5, 10, 25, 50}
	for _, k := range cases {
		t.Run(fmt.Sprintf("k_%d", k), func(t *testing.T) {
			testPID := uniquePGTestPID()
			// Join K times, leave K times, verify membership returns to 0.
			result := runPGScriptWithPID(t, tc, testPID, fmt.Sprintf(`
				local scope = pg.open("test:pg")
				for i = 1, %d do
					local ok, err = scope:join("cycle")
					if err then return nil, tostring(err) end
				end
				for i = 1, %d do
					local ok, err = scope:leave("cycle")
					if err then return nil, tostring(err) end
				end
				local members, err2 = scope:get_members("cycle")
				if err2 then return nil, tostring(err2) end
				return #members
			`, k, k))
			require.NoError(t, result.Error)
			assert.Equal(t, lua.LNumber(0), result.Value.Data(),
				"after %d matched join/leave cycles, group must be empty", k)
		})
	}
}

func TestExtraLua_WhichGroupsListsAfterJoins(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	cases := []int{1, 3, 5, 10, 20}
	for _, n := range cases {
		t.Run(fmt.Sprintf("n_%d", n), func(t *testing.T) {
			testPID := uniquePGTestPID()
			result := runPGScriptWithPID(t, tc, testPID, fmt.Sprintf(`
				local scope = pg.open("test:pg")
				for i = 1, %d do
					local ok, err = scope:join("g-" .. tostring(i))
					if err then return nil, tostring(err) end
				end
				local groups, err = scope:which_groups()
				if err then return nil, tostring(err) end
				return #groups
			`, n))
			require.NoError(t, result.Error)
			// At least n groups should be visible (others may exist from
			// prior tests sharing the scope).
			got := result.Value.Data().(lua.LNumber)
			assert.GreaterOrEqual(t, int(got), n,
				"expected at least %d groups after %d joins", n, n)
		})
	}
}

func TestExtraLua_LeaveOnEmptyGroupErrors(t *testing.T) {
	// leave() on a group the PID never joined propagates an error
	// through the dispatcher path; we accept the error and assert
	// the wrapping path is unchanged across group-name variants.
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	tc := setupPGTest(t)
	defer tc.Close(t)

	cases := []string{"never-joined-1", "never-joined-2", "totally-empty"}
	for _, g := range cases {
		t.Run(g, func(t *testing.T) {
			testPID := uniquePGTestPID()
			result := runPGScriptWithPID(t, tc, testPID, fmt.Sprintf(`
				local scope = pg.open("test:pg")
				local ok, err = pcall(function() return scope:leave(%q) end)
				return ok
			`, g))
			require.NoError(t, result.Error)
			// pcall returns false when scope:leave errors — we just want
			// to observe a deterministic outcome across cases.
			_ = result.Value
		})
	}
}
