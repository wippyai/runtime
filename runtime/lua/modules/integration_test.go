package modules_test

import (
	"context"
	"io/fs"
	"os"
	"testing"

	fsapi "github.com/ponyruntime/pony/api/fs"
	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	excelmodule "github.com/ponyruntime/pony/runtime/lua/modules/excel"
	fsmodule "github.com/ponyruntime/pony/runtime/lua/modules/fs"
	"github.com/ponyruntime/pony/service/directory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap/zaptest"
)

// Create test Excel workbook
func createTestWorkbook() ([]byte, error) {
	f := excelize.NewFile()

	// Add test sheet and data
	f.NewSheet("TestSheet")
	f.SetCellValue("TestSheet", "A1", "Name")
	f.SetCellValue("TestSheet", "B1", "Age")
	f.SetCellValue("TestSheet", "A2", "Alice")
	f.SetCellValue("TestSheet", "B2", 30)

	// Save to buffer
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func TestExcelFileSystemIntegration(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create filesystem
	dirFS, err := directory.NewDirectoryFS(os.TempDir(), fs.ModePerm, false)
	require.NoError(t, err)

	fsModule := fsmodule.NewFSModule()
	excelModule := excelmodule.NewModule(logger)

	// Create a mock resource registry with our test filesystem
	mockRegistry := &mockResourceRegistry{
		resources: map[registry.ID]resource.Resource[any]{
			registry.ParseID("app:test_fs"): &mockResource{resValue: dirFS},
		},
	}

	// Create a VM with coroutine support
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)
	defer vm.Close()

	// Get the Lua state
	L := vm.State()

	// Register the modules
	L.PreloadModule(fsModule.Name(), fsModule.Loader)
	L.PreloadModule(excelModule.Name(), excelModule.Loader)

	// Create a runner with the coroutine layer
	runner := engine.NewRunner(vm, engine.WithLayer(coroutine.NewCoroutineLayer()))

	// Create a UOW for resource management
	uw, ctx := runner.InitUnitOfWork(t.Context())
	defer func() {
		err := uw.Close()
		assert.NoError(t, err, "Unit of work cleanup failed")
	}()

	// Create registry with the filesystem
	fsRegistry := &mockFSRegistry{
		filesystems: map[string]fsapi.FS{
			"test_fs": dirFS,
		},
	}

	// Add the resource registry to the context
	ctx = resource.WithResources(ctx, mockRegistry)
	ctx = logs.WithLogger(ctx, logger)
	ctx = fsapi.WithFSRegistry(ctx, fsRegistry)

	// Set the context in the Lua state
	L.SetContext(ctx)

	// Create a test Excel file in the mock filesystem
	excelData, err := createTestWorkbook()
	require.NoError(t, err, "Failed to create test workbook")
	f, err := dirFS.OpenFile("test.xlsx", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0777)
	require.NoError(t, err)
	_, err = f.Write(excelData)
	require.NoError(t, err)
	f.Close()

	// Execute the integration test script
	err = L.DoString(`
		local fs = require("fs")
		local excel = require("excel")
		-- Get the filesystem
		local fsObj, err = fs.get("test_fs")
		if err then 
			error("Error getting FS: " .. err)
		end
		
		-- Check if the test file exists
		local exists, err = fsObj:exists("test.xlsx")
		if err or not exists then
			error("Test Excel file does not exist")
		end
		
		-- Open the test Excel file
		local file, err = fsObj:open("test.xlsx", "r")
		if err then
			error("Error opening Excel file: " .. err)
		end
		
		-- Open the workbook
		local wb, err = excel.open(file)
		if err then
			error("Error loading Excel workbook: " .. err)
		end
		file:close()
		
		-- Get sheet list
		local sheets, err = wb:get_sheet_list()
		if err or #sheets == 0 then
			error("Error getting sheets or no sheets found")
		end
		
		-- Use the first sheet
		local sheetName = sheets[1]
		
		-- Read current rows
		local rows, err = wb:get_rows(sheetName)
		if err then
			error("Error reading rows: " .. err)
		end
		
		local original_row_count = #rows
		
		-- Add a new row with modified data
		local newRowNum = #rows + 1
		wb:set_cell_value(sheetName, "A" .. newRowNum, "New Employee")
		wb:set_cell_value(sheetName, "B" .. newRowNum, 42)
		
		-- Write modified workbook back to filesystem
		local file, err = fsObj:open("test.xlsx", "w")
		if err then
			error("Error opening Excel file: " .. err)
		end

		local err = wb:write_to(file)
		if err then
			error("Error writing to file: " .. err)
		end
		
		wb:close()
		file:close()
		
		-- Open the modified file to verify changes
		local verifyFile, err = fsObj:open("test.xlsx", "r")
		if err then
			error("Error opening modified file: " .. err)
		end
		
		local verifyWb, err = excel.open(verifyFile)
		if err then
			error("Error loading modified workbook: " .. err)
		end
		
		local verifyRows, err = verifyWb:get_rows(sheetName)
		if err then
			error("Error reading modified rows: " .. err)
		end
		
		verifyFile:close()
		verifyWb:close()
		
		-- Store results in global variables for verification
		test_results = {
			original_row_count = original_row_count,
			modified_row_count = #verifyRows,
			new_employee_name = verifyRows[newRowNum][1],
			new_employee_age = verifyRows[newRowNum][2],
			sheet_name = sheetName
		}
	`)
	require.NoError(t, err, "Lua script execution failed")

	// Get test results
	resultsTable := L.GetGlobal("test_results").(*lua.LTable)
	originalRowCount := resultsTable.RawGetString("original_row_count").(lua.LNumber)
	modifiedRowCount := resultsTable.RawGetString("modified_row_count").(lua.LNumber)
	newEmployeeName := resultsTable.RawGetString("new_employee_name").(lua.LString)
	newEmployeeAge := resultsTable.RawGetString("new_employee_age").(lua.LString)
	sheetName := resultsTable.RawGetString("sheet_name").(lua.LString)

	// Verify results
	assert.Greater(t, float64(modifiedRowCount), float64(originalRowCount), "Modified workbook should have more rows")
	assert.Equal(t, "New Employee", string(newEmployeeName), "New employee name should match")
	assert.Equal(t, "42", string(newEmployeeAge), "New employee age should match")
	assert.NotEmpty(t, string(sheetName), "Sheet name should not be empty")
}

type mockResource struct {
	resValue any
	released bool
}

func (m *mockResource) Get() (any, error) {
	return m.resValue, nil
}

func (m *mockResource) Release() {
	m.released = true
	return
}

type mockResourceRegistry struct {
	resources map[registry.ID]resource.Resource[any]
}

func (m *mockResourceRegistry) Acquire(
	_ context.Context,
	id registry.ID,
	_ resource.AccessMode,
) (resource.Resource[any], error) {
	res, ok := m.resources[id]
	if !ok {
		return nil, resource.ErrResourceNotFound
	}
	return res, nil
}

func (m *mockResourceRegistry) List() ([]registry.ID, error) {
	ids := make([]registry.ID, 0, len(m.resources))
	for id := range m.resources {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *mockResourceRegistry) Exists(id registry.ID) bool {
	_, ok := m.resources[id]
	return ok
}

type mockFSRegistry struct {
	filesystems map[string]fsapi.FS
}

func (m *mockFSRegistry) GetFS(name string) (fsapi.FS, bool) {
	fsi, ok := m.filesystems[name]
	return fsi, ok
}
