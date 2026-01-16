package code

import (
	"testing"

	"github.com/yuin/gopher-lua/compiler/parse"
	"github.com/yuin/gopher-lua/types"
	"github.com/yuin/gopher-lua/types/contract"
	"github.com/yuin/gopher-lua/types/query"
)

// TestMinimalNarrowing_Contrapositive tests that contrapositive implications work:
// If user == nil (via assert.is_nil), then err != nil
func TestMinimalNarrowing_Contrapositive(t *testing.T) {
	// User-defined fetch_user with error-value pattern
	fetchUserSource := `
local function fetch_user(id)
    if id < 0 then
        return nil, "invalid id"
    end
    return {id = id, name = "test"}
end
return fetch_user
`
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: false}, nil)
	fetchManifest, _, err := tc.Check(fetchUserSource, "fetch.lua", nil)
	if err != nil {
		t.Fatalf("fetch compile failed: %v", err)
	}

	// Check if fetch_user has ErrorValueSpec
	if fetchManifest.Export == nil {
		t.Fatal("fetch manifest.Export is nil")
	}
	if fn, ok := fetchManifest.Export.(*types.FunctionType); ok {
		t.Logf("fetch_user type: %v", fn)
		if fn.Refine != nil {
			if spec, ok := fn.Refine.(*contract.Spec); ok {
				t.Logf("fetch_user Refine.Ensures: %v (count: %d)", spec.Ensures, len(spec.Ensures))
			}
		} else {
			t.Error("fetch_user Refine is nil - ErrorValueSpec not detected!")
		}
	}

	// Now test the contrapositive: assert.is_nil(user) should make err non-nil
	assertSource := `
local M = {}
function M.is_nil(val, msg)
    if val ~= nil then
        error((msg or "assertion failed") .. ": expected nil", 2)
    end
end
return M
`
	assertManifest, _, _ := tc.Check(assertSource, "assert.lua", nil)

	// Error type with kind method
	errType := &types.InterfaceType{
		Name: "Error",
		Methods: map[string]*types.FunctionType{
			"kind": types.NewFunction([]types.Type{types.Self}, []types.Type{types.String}),
		},
	}

	// User type
	userType := &types.RecordType{
		Name: "User",
		Fields: []types.RecordField{
			{Name: "id", Type: types.Number},
		},
	}

	// Build fetch_user with ErrorValueSpec manually to ensure correct types
	fetchUserType := &types.FunctionType{
		Params:  []types.Type{types.Number},
		Returns: []types.Type{userType, types.Optional(errType)},
		Refine:  contract.ErrorValueSpec(),
	}

	testSource := `
local user, err = fetch_user(-1)
assert.is_nil(user)
err:kind()  -- Should work: user is nil => err is not nil (contrapositive)
`
	env := types.NewEnv().
		WithSymbol("fetch_user", fetchUserType).
		WithSymbol("assert", assertManifest.Export)

	chunk, _ := parse.ParseString(testSource, "test.lua")
	diags := types.CheckChunk(
		chunk,
		types.WithStdlib(),
		types.WithEnv(env),
		types.WithSource("test.lua"),
	)

	t.Logf("Diagnostics: %d", len(diags))
	for _, d := range diags {
		t.Logf("  %s: %s (line %d)", d.Severity, d.Message, d.Position.Line)
	}

	hasMethodError := false
	for _, d := range diags {
		if d.Severity == types.SeverityError {
			hasMethodError = true
		}
	}
	if hasMethodError {
		t.Error("Contrapositive narrowing not working: err should be non-nil after assert.is_nil(user)")
	}
}

// TestMinimalNarrowing_WithImports is a minimal reproduction showing that
// assertion narrowing works when imports are properly passed.
func TestMinimalNarrowing_WithImports(t *testing.T) {
	// Step 1: Compile assert module and get manifest
	// Use the exact source from tests/app/src/lib/assert.lua
	assertSource := `
local M = {}
function M.is_nil(val, msg)
    if val ~= nil then
        error((msg or "assertion failed") .. ": expected nil, got " .. tostring(val), 2)
    end
end
function M.not_nil(val, msg)
    if val == nil then
        error((msg or "assertion failed") .. ": expected non-nil value", 2)
    end
end
return M
`
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: false}, nil)
	assertManifest, diags, err := tc.Check(assertSource, "assert.lua", nil)
	if err != nil {
		t.Fatalf("assert compile failed: %v", err)
	}
	for _, d := range diags {
		if d.Severity == types.SeverityError {
			t.Logf("assert error: %s", d.Message)
		}
	}

	if assertManifest.Export == nil {
		t.Fatal("assert manifest.Export is nil - module return type not extracted")
	}
	t.Logf("assert module export: %T %v", assertManifest.Export, assertManifest.Export)

	// Debug: Check if is_nil has Refine set
	if isNilType, ok := query.FieldOrMethod(assertManifest.Export, "is_nil"); ok {
		if fn, ok := isNilType.(*types.FunctionType); ok {
			t.Logf("is_nil function type: %v", fn)
			if fn.Refine != nil {
				if spec, ok := fn.Refine.(*contract.Spec); ok {
					t.Logf("is_nil Refine.Ensures: %v", spec.Ensures)
				} else {
					t.Logf("is_nil Refine type: %T", fn.Refine)
				}
			} else {
				t.Log("is_nil Refine is nil - ensures not detected!")
			}
		} else {
			t.Logf("is_nil is not FunctionType: %T", isNilType)
		}
	} else {
		t.Log("is_nil not found in export")
	}

	// Step 2: Test type check with require() - like real code
	testSource := `
local assert = require("assert2")
local db, err = getDB()
assert.is_nil(err, "should get db")
db:method()  -- Should work if db is narrowed from DB? to DB
`
	// Create DB type and getDB function type
	// Methods need types.Self as first param for : call syntax
	dbType := &types.InterfaceType{
		Name: "DB",
		Methods: map[string]*types.FunctionType{
			"method": types.NewFunction([]types.Type{types.Self}, nil),
		},
	}
	getDBType := &types.FunctionType{
		Params:  nil,
		Returns: []types.Type{types.Optional(dbType), types.Optional(types.LuaError)},
		Refine:  contract.ErrorValueSpec(),
	}

	// Build env with assert2 (from manifest) and getDB
	// Note: require("assert2") looks up "assert2" in the env
	env := types.NewEnv().
		WithSymbol("getDB", getDBType).
		WithSymbol("assert2", assertManifest.Export)

	// Debug: verify getDB has the spec
	t.Logf("getDBType.Refine: %T %v", getDBType.Refine, getDBType.Refine)
	if spec, ok := getDBType.Refine.(*contract.Spec); ok {
		t.Logf("getDB ErrorValueSpec ensures: %v", spec.Ensures)
	}

	// Type check
	chunk, _ := parse.ParseString(testSource, "test.lua")
	diags2 := types.CheckChunk(
		chunk,
		types.WithStdlib(),
		types.WithEnv(env),
		types.WithSource("test.lua"),
	)

	t.Logf("\nDiagnostics with assert module: %d", len(diags2))
	for _, d := range diags2 {
		t.Logf("  %s: %s (line %d)", d.Severity, d.Message, d.Position.Line)
	}

	// Check if we still get "cannot call method on optional value" error
	hasOptionalError := false
	for _, d := range diags2 {
		if d.Message == "cannot call method on optional value without nil check" {
			hasOptionalError = true
		}
	}

	if hasOptionalError {
		t.Error("ERROR: db is still Optional after assert.is_nil(err) - narrowing not working")
		t.Log("This means the implications (err is nil => db is not nil) are not being triggered")
	} else {
		t.Log("SUCCESS: db was properly narrowed after assert.is_nil(err)")
	}
}

// TestMinimalNarrowing_AssertEqComparison tests that assert.eq(#arr >= 1, true)
// extracts the comparison as a fact, enabling arr[1] to be non-optional.
func TestMinimalNarrowing_AssertEqComparison(t *testing.T) {
	// Create assert module with eq function
	assertSource := `
local M = {}
function M.eq(actual, expected, msg)
    if actual ~= expected then
        error((msg or "assertion failed") .. ": expected " .. tostring(expected) .. ", got " .. tostring(actual), 2)
    end
end
return M
`
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: false}, nil)
	assertManifest, _, err := tc.Check(assertSource, "assert.lua", nil)
	if err != nil {
		t.Fatalf("assert compile failed: %v", err)
	}

	// Debug: Check if eq has ParamEquals
	if eqType, ok := query.FieldOrMethod(assertManifest.Export, "eq"); ok {
		if fn, ok := eqType.(*types.FunctionType); ok {
			t.Logf("eq function type: %v", fn)
			if fn.Refine != nil {
				t.Logf("eq Refine: %+v", fn.Refine)
			} else {
				t.Log("eq Refine is nil - ParamEquals not detected!")
			}
		}
	} else {
		t.Error("eq not found in export")
	}

	// Test: assert.eq(#rows >= 1, true) should make rows[1] non-optional
	testSource := `
local rows = getRows()
assert.eq(#rows >= 1, true, "should have rows")
local first = rows[1]  -- Should be non-optional if fact is extracted
`
	// Create getRows function that returns string[]
	rowsType := &types.ArrayType{Element: types.String}
	getRowsType := types.NewFunction(nil, []types.Type{rowsType})

	env := types.NewEnv().
		WithSymbol("getRows", getRowsType).
		WithSymbol("assert", assertManifest.Export)

	chunk, _ := parse.ParseString(testSource, "test.lua")
	diags := types.CheckChunk(
		chunk,
		types.WithStdlib(),
		types.WithEnv(env),
		types.WithSource("test.lua"),
	)

	t.Logf("Diagnostics: %d", len(diags))
	for _, d := range diags {
		t.Logf("  %s: %s (line %d)", d.Severity, d.Message, d.Position.Line)
	}

	// Check if we get "cannot access field on optional" error for rows[1]
	hasOptionalError := false
	for _, d := range diags {
		if d.Severity == types.SeverityError {
			hasOptionalError = true
		}
	}

	if hasOptionalError {
		t.Error("ERROR: rows[1] is still Optional after assert.eq(#rows >= 1, true)")
	} else {
		t.Log("SUCCESS: rows[1] properly narrowed after assert.eq")
	}
}
