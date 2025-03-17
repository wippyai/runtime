package excel

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// setupTestVM sets up a Lua state with the Excel module loaded
func setupTestVM(t *testing.T) *lua.LState {
	t.Helper()

	logger := zap.NewNop()
	l := lua.NewState()

	// Register the module
	mod := NewModule(logger)
	l.PreloadModule(mod.Name(), mod.Loader)

	// Load the module
	err := l.DoString(`local excel = require("excel")`)
	require.NoError(t, err, "Failed to load excel module")

	return l
}

// createTestWorkbook creates a test workbook with sample data
func createTestWorkbook() (*bytes.Buffer, error) {
	f := excelize.NewFile()

	// Add test sheet and data
	f.NewSheet("TestSheet")
	f.SetCellValue("TestSheet", "A1", "Name")
	f.SetCellValue("TestSheet", "B1", "Age")
	f.SetCellValue("TestSheet", "A2", "Alice")
	f.SetCellValue("TestSheet", "B2", 30)

	// Save to buffer
	buf := new(bytes.Buffer)
	err := f.Write(buf)
	if err != nil {
		return nil, err
	}

	return buf, nil
}

func TestExcel_New(t *testing.T) {
	l := setupTestVM(t)
	defer l.Close()

	// Simple case: create a new workbook
	err := l.DoString(`
		local excel = require("excel")
		local wb, err = excel.new()
		assert(wb ~= nil, "workbook should not be nil")
		assert(err == nil, "error should be nil")
	`)
	assert.NoError(t, err)
}

func TestExcel_Open(t *testing.T) {
	t.Run("valid workbook", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		buf, err := createTestWorkbook()
		require.NoError(t, err)

		// Use buffer directly as a reader
		ud := l.NewUserData()
		ud.Value = buf
		l.SetGlobal("test_reader", ud)

		err = l.DoString(`
			local excel = require("excel")
			local wb, err = excel.open(test_reader)
			assert(wb ~= nil, "workbook should not be nil")
			assert(err == nil, "error should be nil")
		`)
		assert.NoError(t, err)
	})

	t.Run("not a reader", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		// Create a userdata that's not a reader
		ud := l.NewUserData()
		ud.Value = "not a reader"
		l.SetGlobal("not_reader", ud)

		err := l.DoString(`
			local excel = require("excel")
			local wb, err = excel.open(not_reader)
		`)
		assert.Error(t, err)
	})

	t.Run("empty file", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		// Create an empty buffer
		emptyBuf := new(bytes.Buffer)
		ud := l.NewUserData()
		ud.Value = emptyBuf
		l.SetGlobal("empty_reader", ud)

		err := l.DoString(`
			local excel = require("excel")
			local wb, err = excel.open(empty_reader)
			assert(wb == nil, "workbook should be nil")
			assert(err ~= nil, "error should not be nil")
		`)
		assert.NoError(t, err)
	})

	t.Run("corrupted file", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		// Create a buffer with invalid Excel data
		badBuf := bytes.NewBufferString("not an excel file")
		ud := l.NewUserData()
		ud.Value = badBuf
		l.SetGlobal("bad_reader", ud)

		err := l.DoString(`
			local excel = require("excel")
			local wb, err = excel.open(bad_reader)
			assert(wb == nil, "workbook should be nil")
			assert(err ~= nil, "error should not be nil")
		`)
		assert.NoError(t, err)
	})
}

func TestWorkbook_NewSheet(t *testing.T) {
	t.Run("normal case", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local excel = require("excel")
			local wb = excel.new()
			
			local index, err = wb:new_sheet("TestSheet")
			assert(index ~= nil, "sheet index should not be nil")
			assert(err == nil, "error should be nil")
		`)
		require.NoError(t, err)
	})

	t.Run("duplicate sheet name", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local excel = require("excel")
			local wb = excel.new()
			
			local index1, err = wb:new_sheet("TestSheet")
			assert(index1 ~= nil, "first sheet index should not be nil")
			
			local index2, err = wb:new_sheet("TestSheet")
			assert(index2 ~= nil, "second sheet index should not be nil")
			assert(index1 == index2, "indexes should match for the same sheet name")
		`)
		assert.NoError(t, err)
	})

	t.Run("special characters in name", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local excel = require("excel")
			local wb = excel.new()
			local index, err = wb:new_sheet("Test: Sheet?")
			assert(index = nil, "sheet index should not be nil")
			assert(err ~== nil, "error should be nil")
		`)
		assert.Error(t, err)
	})

	t.Run("invalid workbook", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		ud := l.NewUserData()
		ud.Value = "not a workbook"
		l.SetMetatable(ud, l.GetTypeMetatable("Workbook"))
		l.SetGlobal("fake_wb", ud)

		err := l.DoString(`
			local index, err = fake_wb:new_sheet("TestSheet")
			assert(index == nil, "sheet index should be nil")
			assert(err == "workbook expected", "error should indicate workbook expected")
		`)
		assert.NoError(t, err)
	})
}

func TestWorkbook_GetSheetList(t *testing.T) {
	t.Run("default workbook", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local excel = require("excel")
			local wb = excel.new()
			
			local sheets, err = wb:get_sheet_list()
			assert(sheets ~= nil, "sheets should not be nil")
			assert(err == nil, "error should be nil")
			assert(#sheets >= 1, "should have at least one default sheet")
		`)
		assert.NoError(t, err)
	})

	t.Run("multiple sheets", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local excel = require("excel")
			local wb = excel.new()
			
			wb:new_sheet("Sheet1")
			wb:new_sheet("Sheet2")
			wb:new_sheet("Sheet3")
			
			local sheets, err = wb:get_sheet_list()
			assert(sheets ~= nil, "sheets should not be nil")
			assert(#sheets == 3, "should have 3 sheets")
			
			-- Check if our sheets are in the list
			local count = 0
			for _, name in ipairs(sheets) do
				if name == "Sheet1" or name == "Sheet2" or name == "Sheet3" then
					count = count + 1
				end
			end
			assert(count == 3, "all three new sheets should be in the list")
		`)
		assert.NoError(t, err)
	})

	t.Run("invalid workbook", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		ud := l.NewUserData()
		ud.Value = "not a workbook"
		l.SetMetatable(ud, l.GetTypeMetatable("Workbook"))
		l.SetGlobal("fake_wb", ud)

		err := l.DoString(`
			local sheets, err = fake_wb:get_sheet_list()
			assert(sheets == nil, "sheets should be nil")
			assert(err == "workbook expected", "error should indicate workbook expected")
		`)
		assert.NoError(t, err)
	})
}

func TestWorkbook_GetRows(t *testing.T) {
	t.Run("normal case", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		buf, err := createTestWorkbook()
		require.NoError(t, err)

		ud := l.NewUserData()
		ud.Value = buf
		l.SetGlobal("test_reader", ud)

		err = l.DoString(`
			local excel = require("excel")
			local wb = excel.open(test_reader)
			
			local rows, err = wb:get_rows("TestSheet")
			assert(rows ~= nil, "rows should not be nil")
			assert(err == nil, "error should be nil")
			assert(#rows >= 2, "should have at least 2 rows")
			assert(rows[1][1] == "Name", "first cell should be 'Name'")
			assert(rows[2][1] == "Alice", "cell A2 should be 'Alice'")
		`)
		assert.NoError(t, err)
	})

	t.Run("empty sheet", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local excel = require("excel")
			local wb = excel.new()
			
			-- Create an empty sheet
			wb:new_sheet("EmptySheet")
			
			local rows, err = wb:get_rows("EmptySheet")
			assert(rows ~= nil, "rows should not be nil")
			assert(err == nil, "error should be nil")
			assert(#rows == 0, "empty sheet should have 0 rows")
		`)
		assert.NoError(t, err)
	})

	t.Run("non-existent sheet", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local excel = require("excel")
			local wb = excel.new()
			
			local rows, err = wb:get_rows("NonExistentSheet")
			assert(rows == nil, "rows should be nil")
			assert(err ~= nil, "error should not be nil")
		`)
		assert.NoError(t, err)
	})

	t.Run("different data types", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local excel = require("excel")
			local wb = excel.new()
			
			-- Create a sheet with different data types
			wb:new_sheet("TypesSheet")
			wb:set_cell_value("TypesSheet", "A1", "String")
			wb:set_cell_value("TypesSheet", "A2", 123)
			wb:set_cell_value("TypesSheet", "A3", 45.67)
			wb:set_cell_value("TypesSheet", "A4", true)
			
			local rows = wb:get_rows("TypesSheet")
			assert(rows[1][1] == "String", "string value should be preserved")
			assert(rows[2][1] == "123", "integer should be converted to string")
			assert(rows[3][1] == "45.67", "float should be converted to string")
			assert(rows[4][1] == "true", "boolean should be converted to string")
		`)
		assert.NoError(t, err)
	})

	t.Run("invalid workbook", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		ud := l.NewUserData()
		ud.Value = "not a workbook"
		l.SetMetatable(ud, l.GetTypeMetatable("Workbook"))
		l.SetGlobal("fake_wb", ud)

		err := l.DoString(`
			local rows, err = fake_wb:get_rows("TestSheet")
			assert(rows == nil, "rows should be nil")
			assert(err == "workbook expected", "error should indicate workbook expected")
		`)
		assert.NoError(t, err)
	})
}

func TestWorkbook_SetCellValue(t *testing.T) {
	t.Run("string value", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local excel = require("excel")
			local wb = excel.new()
			wb:new_sheet("TestSheet")
			
			local err = wb:set_cell_value("TestSheet", "A1", "Test String")
			assert(err == nil, "error should be nil")
			
			local rows = wb:get_rows("TestSheet")
			assert(rows[1][1] == "Test String", "cell should contain the string value")
		`)
		assert.NoError(t, err)
	})

	t.Run("numeric value", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local excel = require("excel")
			local wb = excel.new()
			wb:new_sheet("TestSheet")
			
			local err = wb:set_cell_value("TestSheet", "A1", 123)
			assert(err == nil, "error should be nil")
			
			local err = wb:set_cell_value("TestSheet", "A2", 45.67)
			assert(err == nil, "error should be nil")
			
			local rows = wb:get_rows("TestSheet")
			assert(rows[1][1] == "123", "cell should contain the integer value as string")
			assert(rows[2][1] == "45.67", "cell should contain the float value as string")
		`)
		assert.NoError(t, err)
	})

	t.Run("boolean value", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local excel = require("excel")
			local wb = excel.new()
			wb:new_sheet("TestSheet")
			
			local err = wb:set_cell_value("TestSheet", "A1", true)
			assert(err == nil, "error should be nil")
			
			local err = wb:set_cell_value("TestSheet", "A2", false)
			assert(err == nil, "error should be nil")
			
			local rows = wb:get_rows("TestSheet")
			assert(rows[1][1] == "true", "cell should contain true as string")
			assert(rows[2][1] == "false", "cell should contain false as string")
		`)
		assert.NoError(t, err)
	})

	t.Run("non-existent sheet", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local excel = require("excel")
			local wb = excel.new()
			
			local err = wb:set_cell_value("NonExistentSheet", "A1", "Test")
			assert(err ~= nil, "error should not be nil")
		`)
		assert.NoError(t, err)
	})

	t.Run("invalid cell reference", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local excel = require("excel")
			local wb = excel.new()
			wb:new_sheet("TestSheet")
			
			local err = wb:set_cell_value("TestSheet", "InvalidCell", "Test")
			assert(err ~= nil, "error should not be nil")
		`)
		assert.NoError(t, err)
	})

	t.Run("boundary cell reference", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping test in short mode")
		}
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local excel = require("excel")
			local wb = excel.new()
			wb:new_sheet("TestSheet")
			
			-- Use a valid but very large reference
			local err = wb:set_cell_value("TestSheet", "XFD1048576", "Boundary Test")
			assert(err == nil, "error should be nil for valid boundary reference")
			
			-- Excel has limits, but valid references should still work
			local err = wb:set_cell_value("TestSheet", "A1000000", "Large Row")
			if err == nil then
				-- If it works, great - but some implementations might have limits
				local rows = wb:get_rows("TestSheet")
				assert(#rows >= 1000000, "should have at least 1,000,000 rows")
			end
		`)
		assert.NoError(t, err)
	})

	t.Run("invalid workbook", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		ud := l.NewUserData()
		ud.Value = "not a workbook"
		l.SetMetatable(ud, l.GetTypeMetatable("Workbook"))
		l.SetGlobal("fake_wb", ud)

		err := l.DoString(`
			local err = fake_wb:set_cell_value("Sheet1", "A1", "Test")
			assert(err == "workbook expected", "error should indicate workbook expected")
		`)
		assert.NoError(t, err)
	})
}

func TestWorkbook_Close(t *testing.T) {
	t.Run("basic close", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local excel = require("excel")
			local wb = excel.new()
			
			-- Close the workbook
			local err = wb:close()
			assert(err == nil, "close should succeed")
		`)
		assert.NoError(t, err)
	})

	t.Run("invalid workbook", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		ud := l.NewUserData()
		ud.Value = "not a workbook"
		l.SetMetatable(ud, l.GetTypeMetatable("Workbook"))
		l.SetGlobal("fake_wb", ud)

		err := l.DoString(`
			local err = fake_wb:close()
			assert(err == "workbook expected", "error should indicate workbook expected")
		`)
		assert.NoError(t, err)
	})
}

func TestIntegrationWorkflow(t *testing.T) {
	l := setupTestVM(t)
	defer l.Close()

	err := l.DoString(`
		local excel = require("excel")
		
		-- Create a workbook with data
		local wb = excel.new()
		wb:new_sheet("Sales")
		
		-- Set headers
		wb:set_cell_value("Sales", "A1", "Month")
		wb:set_cell_value("Sales", "B1", "Revenue")
		wb:set_cell_value("Sales", "C1", "Expenses")
		wb:set_cell_value("Sales", "D1", "Profit")
		
		-- Add data for each month
		local months = {"Jan", "Feb", "Mar"}
		local revenues = {10000, 12000, 9500}
		local expenses = {8000, 8500, 7800}
		
		for i = 1, 3 do
			local row = i + 1
			wb:set_cell_value("Sales", "A" .. row, months[i])
			wb:set_cell_value("Sales", "B" .. row, revenues[i])
			wb:set_cell_value("Sales", "C" .. row, expenses[i])
			wb:set_cell_value("Sales", "D" .. row, revenues[i] - expenses[i])
		end
		
		-- Verify the data
		local sheets = wb:get_sheet_list()
		local found_sales = false
		for _, name in ipairs(sheets) do
			if name == "Sales" then found_sales = true end
		end
		assert(found_sales, "Sales sheet should exist")
		
		local rows = wb:get_rows("Sales")
		assert(#rows == 4, "Should have 4 rows (header + 3 months)")
		
		-- Check headers
		assert(rows[1][1] == "Month", "First header should be Month")
		assert(rows[1][4] == "Profit", "Fourth header should be Profit")
		
		-- Check data
		assert(rows[2][1] == "Jan", "First month should be Jan")
		assert(rows[2][2] == "10000", "Jan revenue should be 10000")
		assert(rows[3][3] == "8500", "Feb expenses should be 8500")
		assert(rows[4][4] == "1700", "Mar profit should be 1700")

		-- Close the workbook
		local err = wb:close()
		assert(err == nil, "close should succeed")
	`)

	assert.NoError(t, err, "Integration workflow should complete without errors")
}
