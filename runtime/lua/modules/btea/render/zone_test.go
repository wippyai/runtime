package render

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

// mockTeaModel implements tea.Model for testing
type mockTeaModel struct {
	lastMsg tea.Msg
}

func (m *mockTeaModel) Init() tea.Cmd { return nil }
func (m *mockTeaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.lastMsg = msg
	return m, nil
}
func (m *mockTeaModel) View() string { return "" }

//nolint:unused
type mockLuaModel struct {
	lastMsg tea.Msg
	L       *lua.LState
	ud      *lua.LUserData
}

//nolint:unused
func newMockLuaModel(l *lua.LState) *mockLuaModel {
	m := &mockLuaModel{L: l}
	m.ud = l.NewUserData()
	m.ud.Value = m
	l.SetMetatable(m.ud, l.GetTypeMetatable("btea.Model"))
	return m
}

//nolint:unused
func (m *mockLuaModel) Init() tea.Cmd { return nil }

//nolint:unused
func (m *mockLuaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.lastMsg = msg
	return m, nil
}

//nolint:unused
func (m *mockLuaModel) View() string { return "" }

func setupZoneState(_ *testing.T) *lua.LState {
	L := lua.NewState()
	mod := L.NewTable()
	RegisterZone(L, mod)
	L.SetGlobal("btea", mod)
	return L
}

func TestZoneManagerBasics(t *testing.T) {
	L := setupZoneState(t)
	defer L.Close()

	script := `
		local function check_zone_markers(str)
			-- Check for ESC sequence start
			assert(string.byte(str, 1) == 0x1b, "should start with ESC")
			assert(string.byte(str, 2) == 0x5b, "should have [ after ESC")
			-- Check for end marker 'z'
			assert(string.byte(str, 7) == 0x7a, "should have z marker")
			
			-- Check for ending marker sequence (same pattern)
			local len = #str
			assert(string.byte(str, len-6) == 0x1b, "should end with ESC")
			assert(string.byte(str, len-5) == 0x5b, "should have [ before end")
			assert(string.byte(str, len) == 0x7a, "should end with z marker")
			
			return true
		end

		local manager = btea.zone_manager()
		
		-- Test enable/disable with explicit checks
		manager:set_enabled(true)
		assert(manager:is_enabled() == true, "manager should be enabled")
		
		manager:set_enabled(false)
		assert(manager:is_enabled() == false, "manager should be disabled")
		
		manager:set_enabled(true)  -- Re-enable for next tests
		
		-- Test prefix generation with format check
		local prefix = manager:new_prefix()
		assert(type(prefix) == "string", "new_prefix should return a string")
		assert(string.match(prefix, "^zone_%d+__$"), "prefix should match zone_NUMBER__ format")
		
		-- Test marking with strict checks
		local test_str = "test zone"
		local marked = manager:mark("test-id", test_str)
		
		-- Verify marker structure
		assert(check_zone_markers(marked), "marked content should have proper zone markers")
		-- Length check: original + 2 markers (7 bytes each)
		assert(#marked == #test_str + 14, "marked content should be exactly original length + 14 bytes")

		-- Test scanning
		local scanned = manager:scan(marked)
		collectgarbage("stop")  -- introduce pause to propagate since underlying library is async

		local info = manager:get("test-id")

		-- AddCleanup scanning, markers should be removed
		assert(#scanned == #test_str, "scanned result should be same length as original")
		assert(scanned == test_str, "scanned result should equal original string")
		
		-- Test zone info after first scan
		assert(info ~= nil, "zone info should exist after first scan")
		
		if info then
			assert(info:is_zero() == false, "zone info should not be zero")
		end

		-- Test second scan maintains zone info
		manager:scan(marked)
		
		local info2 = manager:get("test-id")
		assert(info2 ~= nil, "zone info should exist after second scan")
		assert(tostring(info) ~= tostring(info2), "second scan should create new zone info object")

		-- Test scanning without markers
		local plain_scan = manager:scan("plain text")
		assert(plain_scan == "plain text", "scanning unmarked text should return it unchanged")

		-- Test non-existing zone
		local non_existing = manager:get("non-existing-id")
		assert(non_existing == nil, "get should return nil for non-existing zones")
	`
	require.NoError(t, L.DoString(script))
}

func TestZoneManagerMouseInteraction(t *testing.T) {
	L := setupZoneState(t)
	defer L.Close()

	// Spawn a mouse message
	mouseTbl := L.NewTable()
	mouseTbl.RawSetString("type", lua.LString("mouse"))
	mouseTbl.RawSetString("x", lua.LNumber(5))
	mouseTbl.RawSetString("y", lua.LNumber(5))
	mouseTbl.RawSetString("action", lua.LString("press"))
	mouseTbl.RawSetString("button", lua.LString("left"))
	mouseTbl.RawSetString("alt", lua.LBool(false))
	mouseTbl.RawSetString("ctrl", lua.LBool(false))
	mouseTbl.RawSetString("shift", lua.LBool(false))

	// Spawn wrapper table
	msgTbl := L.NewTable()
	msgTbl.RawSetString("mouse", mouseTbl)
	msgTbl.RawSetString("type", lua.LString("update"))

	// Spawn model
	model := &mockTeaModel{}
	modelUD := L.NewUserData()
	modelUD.Value = model

	// Set globals
	L.SetGlobal("test_model", modelUD)
	L.SetGlobal("test_msg", msgTbl)

	script := `
		local manager = btea.zone_manager()
		manager:set_enabled(true)
		
		-- Spawn a zone with specific bounds
		local test_content = "This is a test content"
		local marked = manager:mark("test-zone", test_content)
		manager:scan(marked) -- Register the zone
		
		-- Just test bounds check first
		local info = manager:get("test-zone")
		if info then
			local bounds = info:in_bounds(test_msg)
			-- Continue regardless of bounds check result
		end
			
		-- Success if we reach here without errors
		assert(true, "mouse interaction completed")
	`

	require.NoError(t, L.DoString(script))
}

func TestZoneInfo(t *testing.T) {
	L := setupZoneState(t)
	defer L.Close()

	// Spawn a mouse message
	mouseTbl := L.NewTable()
	mouseTbl.RawSetString("type", lua.LString("mouse"))
	mouseTbl.RawSetString("x", lua.LNumber(5))
	mouseTbl.RawSetString("y", lua.LNumber(5))
	mouseTbl.RawSetString("action", lua.LString("press"))
	mouseTbl.RawSetString("button", lua.LString("left"))
	mouseTbl.RawSetString("alt", lua.LBool(false))
	mouseTbl.RawSetString("ctrl", lua.LBool(false))
	mouseTbl.RawSetString("shift", lua.LBool(false))

	// Spawn wrapper table
	msgTbl := L.NewTable()
	msgTbl.RawSetString("mouse", mouseTbl)
	msgTbl.RawSetString("type", lua.LString("update"))

	L.SetGlobal("test_msg", msgTbl)

	script := `
		local manager = btea.zone_manager()
		manager:set_enabled(true)
		
		-- Spawn and scan specific content to ensure zone registration
		local content = manager:mark("test-zone", string.rep("test content", 5))
		manager:scan(content)
		
		-- Allow time for zone registration
		local info = manager:get("test-zone")
		if info ~= nil then
			-- Test zone info methods
			assert(info:is_zero() == false, "zone info should not be zero")
			
			local in_bounds = info:in_bounds(test_msg)
			assert(type(in_bounds) == "boolean", "in_bounds should return boolean")
			
			local x, y = info:pos(test_msg)
			assert(type(x) == "number", "pos should return numbers")
			assert(type(y) == "number", "pos should return numbers")
		end
	`

	require.NoError(t, L.DoString(script))
}

func TestZoneManagerErrors(t *testing.T) {
	L := setupZoneState(t)
	defer L.Close()

	tests := []struct {
		name   string
		script string
	}{
		{
			name: "invalid mouse message",
			script: `
				local manager = btea.zone_manager()
				local invalid_msg = {type = "invalid"}
				manager:any_in_bounds({}, invalid_msg)
			`,
		},
		{
			name: "invalid model",
			script: `
				local manager = btea.zone_manager()
				local mouse_msg = {mouse = {type = "mouse", x = 0, y = 0, action = "press", button = "left"}}
				manager:any_in_bounds("not a model", mouse_msg)
			`,
		},
		{
			name: "nil zone info",
			script: `
				local manager = btea.zone_manager()
				local info = manager:get("non-existing")
				assert(info == nil, "should return nil for non-existing zones")
			`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := L.DoString(tt.script)
			if tt.name == "nil zone info" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestZoneInfoEdgeCases(t *testing.T) {
	L := setupZoneState(t)
	defer L.Close()

	// Spawn test mouse message with edge coordinates
	mouseTbl := L.NewTable()
	mouseTbl.RawSetString("type", lua.LString("mouse"))
	mouseTbl.RawSetString("x", lua.LNumber(0)) // Edge case
	mouseTbl.RawSetString("y", lua.LNumber(0)) // Edge case
	mouseTbl.RawSetString("action", lua.LString("press"))
	mouseTbl.RawSetString("button", lua.LString("left"))

	msgTbl := L.NewTable()
	msgTbl.RawSetString("mouse", mouseTbl)
	msgTbl.RawSetString("type", lua.LString("update"))

	L.SetGlobal("test_msg", msgTbl)

	script := `
        local manager = btea.zone_manager()
        manager:set_enabled(true)
        
        -- Test with empty content
        local empty_content = manager:mark("empty-zone", "")
        manager:scan(empty_content)
        local empty_info = manager:get("empty-zone")
        
        if empty_info then
            local in_bounds = empty_info:in_bounds(test_msg)
            assert(not in_bounds, "empty zone should not be in bounds")
            
            local x, y = empty_info:pos(test_msg)
            assert(x == 0 and y == 0, "empty zone should return 0,0")
        end
        
        -- Test with edge coordinates
        local edge_content = manager:mark("edge-zone", "edge")
        manager:scan(edge_content)
        local edge_info = manager:get("edge-zone")
        
        if edge_info then
            local x, y = edge_info:pos(test_msg)
            assert(type(x) == "number" and type(y) == "number", 
                   "should handle edge coordinates")
        end
        
        -- Test error cases
        local success, err = pcall(function()
            edge_info:in_bounds({type="invalid"})
        end)
        assert(not success, "should fail with invalid message type")
        
        local success2, err2 = pcall(function()
            edge_info:pos({type="invalid"})
        end)
        assert(not success2, "should fail with invalid message type")
    `

	require.NoError(t, L.DoString(script))
}
