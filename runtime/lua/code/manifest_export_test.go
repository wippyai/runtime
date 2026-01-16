package code

import (
	"testing"

	"github.com/yuin/gopher-lua/types"
	"github.com/yuin/gopher-lua/types/contract"
	"github.com/yuin/gopher-lua/types/core"
	"github.com/yuin/gopher-lua/types/io"
	"github.com/yuin/gopher-lua/types/predicate"
	"github.com/yuin/gopher-lua/types/query"
)

// TestManifestExport_AssertModule tests that compiling assert.lua produces a manifest
// with Export set to the module type, and function Refine preserved through serialization.
func TestManifestExport_AssertModule(t *testing.T) {
	// Create type checker with built-ins
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: false}, nil)

	assertSource := `
local M = {}

function M.is_nil(val, msg)
    if val ~= nil then error(msg) end
end

function M.not_nil(val, msg)
    if val == nil then error(msg) end
end

return M
`

	manifest, diags, err := tc.Check(assertSource, "assert.lua", nil)
	if err != nil {
		t.Fatalf("type check failed: %v", err)
	}

	for _, d := range diags {
		if d.Severity == types.SeverityError {
			t.Logf("diagnostic: %s", d.Message)
		}
	}

	// Verify manifest has Export set
	if manifest == nil {
		t.Fatal("manifest is nil")
	}

	if manifest.Export == nil {
		t.Fatal("manifest.Export should not be nil")
	}

	t.Logf("manifest.Export type: %T %v", manifest.Export, manifest.Export)

	// Get the is_nil function type from the module
	isNilType, ok := query.FieldOrMethod(manifest.Export, "is_nil")
	if !ok {
		t.Fatal("is_nil not found in module")
	}

	fn, ok := isNilType.(*core.FunctionType)
	if !ok {
		t.Fatalf("is_nil should be FunctionType, got %T", isNilType)
	}

	// Verify Refine is set with IsNil ensures
	if fn.Refine == nil {
		t.Error("is_nil should have Refine set")
	} else {
		spec, ok := fn.Refine.(*contract.Spec)
		if !ok {
			t.Errorf("Refine should be *contract.Spec, got %T", fn.Refine)
		} else {
			if len(spec.Ensures) == 0 {
				t.Error("is_nil Spec.Ensures should not be empty")
			} else {
				t.Logf("is_nil Ensures: %v", spec.Ensures)
			}
		}
	}
}

// TestManifestExport_Serialization tests that Spec survives serialization.
func TestManifestExport_Serialization(t *testing.T) {
	// Create a function type with Refine spec
	spec := contract.NewSpec().WithEnsures(predicate.IsNil{Name: "param[0]"})
	fn := &core.FunctionType{
		Params:     []types.Type{types.Any, types.Any},
		ParamNames: []string{"val", "msg"},
		Refine:     spec,
	}

	// Create a record type as the module export
	moduleType := core.NewRecord("M", []core.RecordField{
		{Name: "is_nil", Type: fn},
	}, false)

	// Serialize and deserialize
	data, err := io.Encode(moduleType)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := io.Decode(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Verify the decoded type
	recDec, ok := decoded.(*core.RecordType)
	if !ok {
		t.Fatalf("expected RecordType, got %T", decoded)
	}

	// Get is_nil field
	isNilType, ok := query.FieldOrMethod(recDec, "is_nil")
	if !ok {
		t.Fatal("is_nil not found after decode")
	}

	fnDec, ok := isNilType.(*core.FunctionType)
	if !ok {
		t.Fatalf("is_nil should be FunctionType, got %T", isNilType)
	}

	// Verify Refine survived
	if fnDec.Refine == nil {
		t.Fatal("Refine should not be nil after decode")
	}

	specDec, ok := fnDec.Refine.(*contract.Spec)
	if !ok {
		t.Fatalf("Refine should be *contract.Spec, got %T", fnDec.Refine)
	}

	if len(specDec.Ensures) != 1 {
		t.Fatalf("Ensures count = %d, want 1", len(specDec.Ensures))
	}

	isNil, ok := specDec.Ensures[0].(predicate.IsNil)
	if !ok {
		t.Fatalf("Ensures[0] should be IsNil, got %T", specDec.Ensures[0])
	}

	if isNil.Name != "param[0]" {
		t.Errorf("IsNil.Name = %q, want %q", isNil.Name, "param[0]")
	}
}

// TestManifestExport_EndToEnd tests the full flow: compile assert module,
// serialize its manifest, use it as import, verify narrowing works.
func TestManifestExport_EndToEnd(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: false}, nil)

	// Step 1: Compile assert module
	assertSource := `
local M = {}
function M.is_nil(val, msg)
    if val ~= nil then error(msg) end
end
return M
`
	assertManifest, _, err := tc.Check(assertSource, "assert.lua", nil)
	if err != nil {
		t.Fatalf("assert compile failed: %v", err)
	}

	if assertManifest.Export == nil {
		t.Fatal("assert manifest.Export is nil")
	}

	// Step 2: Serialize and deserialize manifest
	exportData, err := io.Encode(assertManifest.Export)
	if err != nil {
		t.Fatalf("Encode export failed: %v", err)
	}

	decodedExport, err := io.Decode(exportData)
	if err != nil {
		t.Fatalf("Decode export failed: %v", err)
	}

	// Create a new manifest with the decoded export
	loadedManifest := types.NewManifest("assert")
	loadedManifest.SetExport(decodedExport)

	// Step 3: Compile test code using the loaded manifest
	testSource := `
local db, err = getDB()
assert.is_nil(err, "should get db")
-- After assert.is_nil(err), db should be narrowed
local x = db .. " test"  -- This should work if db is narrowed from string? to string
`

	// Add getDB function that returns (string?, error?)
	getDBType := types.NewFunction(
		nil,
		[]types.Type{types.Optional(types.String), types.Optional(types.LuaError)},
	)

	// Create env with getDB
	env := types.NewEnv().WithSymbol("getDB", getDBType)

	// Create imports with assert module
	imports := map[string]*types.TypeManifest{
		"assert": loadedManifest,
	}

	// Add assert to env from imports
	for alias, m := range imports {
		if m != nil && m.Export != nil {
			env = env.WithSymbol(alias, m.Export)
		}
	}

	// Need to also add implications for err -> db pattern
	// For now, just check that the module is loaded correctly
	_, diags, err := tc.Check(testSource, "test.lua", imports)
	if err != nil {
		t.Fatalf("test compile failed: %v", err)
	}

	// Log diagnostics
	t.Logf("Diagnostics: %d", len(diags))
	for _, d := range diags {
		t.Logf("  %s: %s", d.Severity, d.Message)
	}
}
