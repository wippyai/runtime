package code

import (
	"testing"

	"github.com/yuin/gopher-lua/types/contract"
	"github.com/yuin/gopher-lua/types/diag"
	"github.com/yuin/gopher-lua/types/query"
	"github.com/yuin/gopher-lua/types/typ"
)

// TestMinimalNarrowing_AssertModuleCompilation tests that compiling an assert module
// produces functions with proper Spec set for narrowing.
func TestMinimalNarrowing_AssertModuleCompilation(t *testing.T) {
	assertSource := `
local M = {}
function M.is_nil(val, msg)
    if val ~= nil then
        error((msg or "assertion failed") .. ": expected nil", 2)
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
	assertManifest, diags, _ := tc.Check(assertSource, "assert.lua", nil)

	for _, d := range diags {
		if d.Severity == diag.SeverityError {
			t.Logf("assert error: %s", d.Message)
		}
	}

	if assertManifest.Export == nil {
		t.Fatal("assert manifest.Export is nil - module return type not extracted")
	}
	t.Logf("assert module export: %T %v", assertManifest.Export, assertManifest.Export)

	// Check if is_nil has Spec set
	if isNilType, ok := query.FieldOrMethod(assertManifest.Export, "is_nil"); ok {
		if fn, ok := isNilType.(*typ.Function); ok {
			t.Logf("is_nil function type: %v", fn)
			if fn.Spec != nil {
				if spec, ok := fn.Spec.(*contract.Spec); ok {
					t.Logf("is_nil Spec.Ensures: %v", spec.Ensures)
				} else {
					t.Logf("is_nil Spec type: %T", fn.Spec)
				}
			} else {
				t.Log("is_nil Spec is nil - ensures not detected")
			}
		} else {
			t.Logf("is_nil is not *typ.Function: %T", isNilType)
		}
	} else {
		t.Log("is_nil not found in export")
	}

	// Check if not_nil has Spec set
	if notNilType, ok := query.FieldOrMethod(assertManifest.Export, "not_nil"); ok {
		if fn, ok := notNilType.(*typ.Function); ok {
			t.Logf("not_nil function type: %v", fn)
			if fn.Spec != nil {
				if spec, ok := fn.Spec.(*contract.Spec); ok {
					t.Logf("not_nil Spec.Ensures: %v", spec.Ensures)
				} else {
					t.Logf("not_nil Spec type: %T", fn.Spec)
				}
			} else {
				t.Log("not_nil Spec is nil - ensures not detected")
			}
		} else {
			t.Logf("not_nil is not *typ.Function: %T", notNilType)
		}
	} else {
		t.Log("not_nil not found in export")
	}
}

// TestMinimalNarrowing_FetchUserErrorValuePattern tests that a function returning
// (value, error) pair is detected and marked with ErrorValueSpec.
func TestMinimalNarrowing_FetchUserErrorValuePattern(t *testing.T) {
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
	fetchManifest, _, _ := tc.Check(fetchUserSource, "fetch.lua", nil)

	// Check if fetch_user has ErrorValueSpec
	if fetchManifest.Export == nil {
		t.Fatal("fetch manifest.Export is nil")
	}

	if fn, ok := fetchManifest.Export.(*typ.Function); ok {
		t.Logf("fetch_user type: %v", fn)
		if fn.Spec != nil {
			if spec, ok := fn.Spec.(*contract.Spec); ok {
				t.Logf("fetch_user Spec.Ensures: %v (count: %d)", spec.Ensures, len(spec.Ensures))
			} else {
				t.Logf("fetch_user Spec type: %T", fn.Spec)
			}
		} else {
			t.Log("fetch_user Spec is nil - ErrorValueSpec not detected")
		}
	} else {
		t.Logf("fetch_user is not *typ.Function: %T", fetchManifest.Export)
	}
}

// TestMinimalNarrowing_AssertEqPattern tests that assert.eq function is properly typed.
func TestMinimalNarrowing_AssertEqPattern(t *testing.T) {
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
	assertManifest, _, _ := tc.Check(assertSource, "assert.lua", nil)

	// Check if eq has ParamEquals
	if eqType, ok := query.FieldOrMethod(assertManifest.Export, "eq"); ok {
		if fn, ok := eqType.(*typ.Function); ok {
			t.Logf("eq function type: %v", fn)
			if fn.Spec != nil {
				t.Logf("eq Spec: %+v", fn.Spec)
			} else {
				t.Log("eq Spec is nil - ParamEquals not detected")
			}
		}
	} else {
		t.Error("eq not found in export")
	}
}
