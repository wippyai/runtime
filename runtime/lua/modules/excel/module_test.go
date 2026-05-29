// SPDX-License-Identifier: MPL-2.0

package excel

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/xuri/excelize/v2"
)

func bindExcel(l *lua.LState) {
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
}

func setupTestVM(t *testing.T) *lua.LState {
	t.Helper()
	l := lua.NewState()
	l.SetContext(context.Background())
	lua.OpenErrors(l)
	bindExcel(l)
	return l
}

func createTestWorkbook() (*bytes.Buffer, error) {
	f := excelize.NewFile()
	_, _ = f.NewSheet("TestSheet")
	_ = f.SetCellValue("TestSheet", "A1", "Name")
	_ = f.SetCellValue("TestSheet", "B1", "Age")
	_ = f.SetCellValue("TestSheet", "A2", "Alice")
	_ = f.SetCellValue("TestSheet", "B2", 30)

	buf := new(bytes.Buffer)
	err := f.Write(buf)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func TestLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bindExcel(l)

	mod := l.GetGlobal("excel")
	if mod.Type() != lua.LTTable {
		t.Fatal("module not registered")
	}

	modTbl := mod.(*lua.LTable)
	if modTbl.RawGetString("new").Type() != lua.LTFunction {
		t.Error("new function not registered")
	}
	if modTbl.RawGetString("open").Type() != lua.LTFunction {
		t.Error("open function not registered")
	}
}

func TestLoadReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	bindExcel(l1)
	bindExcel(l2)

	mod1 := l1.GetGlobal("excel").(*lua.LTable)
	mod2 := l2.GetGlobal("excel").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestExcel_New(t *testing.T) {
	l := setupTestVM(t)
	defer l.Close()

	err := l.DoString(`
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

		ud := l.NewUserData()
		ud.Value = buf
		l.SetGlobal("test_reader", ud)

		err = l.DoString(`
			local wb, err = excel.open(test_reader)
			assert(wb ~= nil, "workbook should not be nil")
			assert(err == nil, "error should be nil")
		`)
		assert.NoError(t, err)
	})

	t.Run("not a reader", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		ud := l.NewUserData()
		ud.Value = "not a reader"
		l.SetGlobal("not_reader", ud)

		err := l.DoString(`
			local wb, err = excel.open(not_reader)
		`)
		assert.Error(t, err)
	})

	t.Run("empty file", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		emptyBuf := new(bytes.Buffer)
		ud := l.NewUserData()
		ud.Value = emptyBuf
		l.SetGlobal("empty_reader", ud)

		err := l.DoString(`
			local wb, err = excel.open(empty_reader)
			assert(wb == nil, "workbook should be nil")
			assert(err ~= nil, "error should not be nil")
		`)
		assert.NoError(t, err)
	})

	t.Run("corrupted file", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		badBuf := bytes.NewBufferString("not an excel file")
		ud := l.NewUserData()
		ud.Value = badBuf
		l.SetGlobal("bad_reader", ud)

		err := l.DoString(`
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
			local wb = excel.new()

			local index1, err = wb:new_sheet("TestSheet")
			assert(index1 ~= nil, "first sheet index should not be nil")

			local index2, err = wb:new_sheet("TestSheet")
			assert(index2 ~= nil, "second sheet index should not be nil")
			assert(index1 == index2, "indexes should match for the same sheet name")
		`)
		assert.NoError(t, err)
	})

	t.Run("invalid workbook error kind", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		ud := l.NewUserData()
		ud.Value = "not a workbook"
		ud.Metatable = workbookMetatable
		l.SetGlobal("fake_wb", ud)

		err := l.DoString(`
			local index, err = fake_wb:new_sheet("TestSheet")
			assert(index == nil, "sheet index should be nil")
			assert(err ~= nil, "error should not be nil")
			assert(err:kind() == errors.INVALID, "error kind should be INVALID")
			assert(err:retryable() == false, "error should not be retryable")
		`)
		assert.NoError(t, err)
	})
}

func TestWorkbook_GetSheetList(t *testing.T) {
	t.Run("default workbook", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
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
			local wb = excel.new()

			wb:new_sheet("Sheet1")
			wb:new_sheet("Sheet2")
			wb:new_sheet("Sheet3")

			local sheets, err = wb:get_sheet_list()
			assert(sheets ~= nil, "sheets should not be nil")
			assert(#sheets == 3, "should have 3 sheets")

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
			local wb = excel.new()

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
			local wb = excel.new()

			local rows, err = wb:get_rows("NonExistentSheet")
			assert(rows == nil, "rows should be nil")
			assert(err ~= nil, "error should not be nil")
			assert(err:kind() == errors.INTERNAL, "error kind should be INTERNAL")
		`)
		assert.NoError(t, err)
	})

	t.Run("different data types", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local wb = excel.new()

			wb:new_sheet("TypesSheet")
			wb:set_cell_value("TypesSheet", "A1", "String")
			wb:set_cell_value("TypesSheet", "A2", 123)
			wb:set_cell_value("TypesSheet", "A3", 45.67)
			wb:set_cell_value("TypesSheet", "A4", true)

			local rows = wb:get_rows("TypesSheet")
			assert(rows[1][1] == "String", "string value should be preserved")
			assert(rows[2][1] == "123", "integer should be converted to string")
			assert(rows[3][1] == "45.67", "float should be converted to string")
			-- excelize returns "TRUE" not "true"
			assert(rows[4][1] == "TRUE", "boolean should be converted to uppercase string")
		`)
		assert.NoError(t, err)
	})
}

func TestWorkbook_SetCellValue(t *testing.T) {
	t.Run("string value", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
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
			local wb = excel.new()
			wb:new_sheet("TestSheet")

			local err = wb:set_cell_value("TestSheet", "A1", true)
			assert(err == nil, "error should be nil")

			local err = wb:set_cell_value("TestSheet", "A2", false)
			assert(err == nil, "error should be nil")

			local rows = wb:get_rows("TestSheet")
			-- excelize returns "TRUE"/"FALSE" not "true"/"false"
			assert(rows[1][1] == "TRUE", "cell should contain TRUE as string")
			assert(rows[2][1] == "FALSE", "cell should contain FALSE as string")
		`)
		assert.NoError(t, err)
	})

	t.Run("non-existent sheet error", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local wb = excel.new()

			local err = wb:set_cell_value("NonExistentSheet", "A1", "Test")
			assert(err ~= nil, "error should not be nil")
			assert(err:kind() == errors.INTERNAL, "error kind should be INTERNAL")
		`)
		assert.NoError(t, err)
	})
}

func TestWorkbook_Close(t *testing.T) {
	t.Run("basic close", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local wb = excel.new()

			local err = wb:close()
			assert(err == nil, "close should succeed")
		`)
		assert.NoError(t, err)
	})

	t.Run("double close is idempotent", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local wb = excel.new()

			local err1 = wb:close()
			assert(err1 == nil, "first close should succeed")

			local err2 = wb:close()
			assert(err2 == nil, "second close should also succeed")
		`)
		assert.NoError(t, err)
	})

	t.Run("operations after close", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		err := l.DoString(`
			local wb = excel.new()
			wb:close()

			local sheets, err = wb:get_sheet_list()
			assert(sheets == nil, "sheets should be nil after close")
			assert(err ~= nil, "error should not be nil")
			assert(err:kind() == errors.INTERNAL, "error kind should be INTERNAL")
		`)
		assert.NoError(t, err)
	})
}

func TestWorkbook_WriteTo(t *testing.T) {
	t.Run("write to buffer", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		outBuf := new(bytes.Buffer)
		ud := l.NewUserData()
		ud.Value = outBuf
		l.SetGlobal("out_buffer", ud)

		err := l.DoString(`
			local wb = excel.new()

			wb:new_sheet("TestSheet")
			wb:set_cell_value("TestSheet", "A1", "Test Data")
			wb:set_cell_value("TestSheet", "B1", 123)

			local err = wb:write_to(out_buffer)
			assert(err == nil, "write_to should succeed")

			wb:close()
		`)
		assert.NoError(t, err)
		assert.NotEmpty(t, outBuf.Bytes(), "Buffer should contain Excel file data")

		f, err := excelize.OpenReader(bytes.NewReader(outBuf.Bytes()))
		assert.NoError(t, err)

		if f != nil {
			val, err := f.GetCellValue("TestSheet", "A1")
			assert.NoError(t, err)
			assert.Equal(t, "Test Data", val)
			_ = f.Close()
		}
	})

	t.Run("invalid writer error", func(t *testing.T) {
		l := setupTestVM(t)
		defer l.Close()

		ud := l.NewUserData()
		ud.Value = "not a writer"
		l.SetGlobal("invalid_writer", ud)

		err := l.DoString(`
			local wb = excel.new()

			local err = wb:write_to(invalid_writer)
			assert(err ~= nil, "should get error")
			assert(err:kind() == errors.INTERNAL, "error kind should be INTERNAL")

			wb:close()
		`)
		assert.NoError(t, err)
	})
}

func TestIntegrationWorkflow(t *testing.T) {
	l := setupTestVM(t)
	defer l.Close()

	err := l.DoString(`
		local wb = excel.new()
		wb:new_sheet("Sales")

		wb:set_cell_value("Sales", "A1", "Month")
		wb:set_cell_value("Sales", "B1", "Revenue")
		wb:set_cell_value("Sales", "C1", "Expenses")
		wb:set_cell_value("Sales", "D1", "Profit")

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

		local sheets = wb:get_sheet_list()
		local found_sales = false
		for _, name in ipairs(sheets) do
			if name == "Sales" then found_sales = true end
		end
		assert(found_sales, "Sales sheet should exist")

		local rows = wb:get_rows("Sales")
		assert(#rows == 4, "Should have 4 rows (header + 3 months)")

		assert(rows[1][1] == "Month", "First header should be Month")
		assert(rows[1][4] == "Profit", "Fourth header should be Profit")

		assert(rows[2][1] == "Jan", "First month should be Jan")
		assert(rows[2][2] == "10000", "Jan revenue should be 10000")
		assert(rows[3][3] == "8500", "Feb expenses should be 8500")
		assert(rows[4][4] == "1700", "Mar profit should be 1700")

		local err = wb:close()
		assert(err == nil, "close should succeed")
	`)

	assert.NoError(t, err)
}

func TestWriteAndReadBack(t *testing.T) {
	l := setupTestVM(t)
	defer l.Close()

	outBuf := new(bytes.Buffer)
	ud := l.NewUserData()
	ud.Value = outBuf
	l.SetGlobal("out_buffer", ud)

	err := l.DoString(`
		local wb = excel.new()
		wb:new_sheet("Report")

		wb:set_cell_value("Report", "A1", "Sales Report")
		wb:set_cell_value("Report", "A2", "Quarter")
		wb:set_cell_value("Report", "B2", "Amount")

		for i = 1, 4 do
			wb:set_cell_value("Report", "A" .. (i+2), "Q" .. i)
			wb:set_cell_value("Report", "B" .. (i+2), i * 1000)
		end

		local err = wb:write_to(out_buffer)
		assert(err == nil, "write_to should succeed")
		wb:close()
	`)
	require.NoError(t, err)

	inBuf := bytes.NewReader(outBuf.Bytes())
	readerUD := l.NewUserData()
	readerUD.Value = inBuf
	l.SetGlobal("in_buffer", readerUD)

	err = l.DoString(`
		local wb, err = excel.open(in_buffer)
		assert(wb ~= nil, "should be able to open workbook from buffer")
		assert(err == nil, "open should succeed")

		local rows = wb:get_rows("Report")
		assert(rows[1][1] == "Sales Report", "Title should match")
		assert(rows[3][1] == "Q1", "First quarter label should match")
		assert(rows[6][2] == "4000", "Q4 amount should match")

		wb:close()
	`)
	assert.NoError(t, err)
}
