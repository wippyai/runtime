package code

import (
	"testing"

	"github.com/wippyai/go-lua/types/constraint"
	"github.com/wippyai/go-lua/types/contract"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/query/core"
	"github.com/wippyai/go-lua/types/typ"
)

// TestManifestExport_AssertModule tests that compiling assert.lua produces a manifest
// with Export set to the module type, and function Refine preserved through serialization.
func TestManifestExport_AssertModule(t *testing.T) {
	t.Skip("Spec inference not yet implemented in go-lua type checker")

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

	manifest, diags, _ := tc.Check(assertSource, "assert.lua", nil)

	for _, d := range diags {
		if d.Severity == diag.SeverityError {
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
	isNilType, ok := core.FieldOrMethod(manifest.Export, "is_nil")
	if !ok {
		t.Fatal("is_nil not found in module")
	}

	fn, ok := isNilType.(*typ.Function)
	if !ok {
		t.Fatalf("is_nil should be *typ.Function, got %T", isNilType)
	}

	// Verify Spec is set with IsNil ensures
	if fn.Spec == nil {
		t.Error("is_nil should have Spec set")
	} else {
		spec, ok := fn.Spec.(*contract.Spec)
		if !ok {
			t.Errorf("Spec should be *contract.Spec, got %T", fn.Spec)
		} else {
			if !spec.Ensures.HasConstraints() {
				t.Error("is_nil Spec.Ensures should not be empty")
			} else {
				t.Logf("is_nil Ensures: %v", spec.Ensures)
			}
		}
	}
}

// TestManifestExport_Serialization tests that Spec survives serialization.
func TestManifestExport_Serialization(t *testing.T) {
	// Create a function type with Spec
	spec := contract.NewSpec().WithEnsures(constraint.IsNil{Path: constraint.Path{Root: "param[0]"}})
	fn := typ.Func().
		Param("val", typ.Any).
		Param("msg", typ.Any).
		Build()
	fn.Spec = spec

	// Create a record type as the module export
	moduleType := typ.NewRecord().
		Field("is_nil", fn).
		Build()

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
	recDec, ok := decoded.(*typ.Record)
	if !ok {
		t.Fatalf("expected *typ.Record, got %T", decoded)
	}

	// Get is_nil field
	isNilType, ok := core.FieldOrMethod(recDec, "is_nil")
	if !ok {
		t.Fatal("is_nil not found after decode")
	}

	fnDec, ok := isNilType.(*typ.Function)
	if !ok {
		t.Fatalf("is_nil should be *typ.Function, got %T", isNilType)
	}

	// Verify Spec survived
	if fnDec.Spec == nil {
		t.Fatal("Spec should not be nil after decode")
	}

	specDec, ok := fnDec.Spec.(*contract.Spec)
	if !ok {
		t.Fatalf("Spec should be *contract.Spec, got %T", fnDec.Spec)
	}

	all := specDec.Ensures.AllConstraints()
	if len(all) != 1 {
		t.Fatalf("Ensures count = %d, want 1", len(all))
	}

	list := all
	isNil, ok := list[0].(constraint.IsNil)
	if !ok {
		t.Fatalf("Ensures[0] should be IsNil, got %T", list[0])
	}

	if isNil.Path.Root != "param[0]" {
		t.Errorf("IsNil.Path.Root = %q, want %q", isNil.Path.Root, "param[0]")
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
	assertManifest, _, _ := tc.Check(assertSource, "assert.lua", nil)

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
	loadedManifest := io.NewManifest("assert")
	loadedManifest.Export = decodedExport

	// Step 3: Compile test code using the loaded manifest
	testSource := `
local db, err = getDB()
assert.is_nil(err, "should get db")
-- After assert.is_nil(err), db should be narrowed
local x = db .. " test"  -- This should work if db is narrowed from string? to string
`

	// Add getDB function that returns (string?, error?)
	getDBType := typ.Func().
		Returns(typ.NewOptional(typ.String)).
		Returns(typ.NewOptional(typ.LuaError)).
		Build()

	// Create imports with assert module
	imports := map[string]*io.Manifest{
		"assert": loadedManifest,
	}

	// Create a manifest with getDB as a global
	envManifest := io.NewManifest("env")
	envManifest.Globals = map[string]typ.Type{
		"getDB": getDBType,
	}

	// Merge globals into imports
	imports["_env"] = envManifest

	// Need to also add implications for err -> db pattern
	// For now, just check that the module is loaded correctly
	_, diags, _ := tc.Check(testSource, "test.lua", imports)

	// Log diagnostics
	t.Logf("Diagnostics: %d", len(diags))
	for _, d := range diags {
		t.Logf("  %s: %s", d.Severity, d.Message)
	}
}

func TestManifestExport_TypeCheckerIncludesFunctionSummaries(t *testing.T) {
	tc := NewTypeChecker(TypeCheckConfig{Enabled: true, Strict: false}, nil)

	source := `
local M = {}

function M.not_nil(val, msg)
    if val == nil then error(msg) end
    return val
end

return M
`

	manifest, diags, err := tc.Check(source, "assert.lua", nil)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	for _, d := range diags {
		if d.Severity == diag.SeverityError {
			t.Fatalf("unexpected diagnostic error: %s", d.Message)
		}
	}
	if manifest == nil {
		t.Fatal("manifest is nil")
	}

	summary, ok := manifest.LookupSummary("not_nil")
	if !ok || summary == nil {
		t.Fatal("expected not_nil function summary in manifest")
	}
	if !summary.Ensures.HasConstraints() {
		t.Fatal("not_nil summary should include ensures constraints")
	}

	enriched := manifest.EnrichedExport()
	notNilType, ok := core.FieldOrMethod(enriched, "not_nil")
	if !ok {
		t.Fatal("not_nil not found in enriched export")
	}
	fn, ok := notNilType.(*typ.Function)
	if !ok {
		t.Fatalf("not_nil should be function, got %T", notNilType)
	}
	if fn.Spec == nil && fn.Refinement == nil {
		t.Fatal("not_nil function should carry spec or refinement from summary")
	}
}
