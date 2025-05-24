package text

import (
	"time"

	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/sergi/go-diff/diffmatchpatch"
	lua "github.com/yuin/gopher-lua"
)

// Diff operation constants
const (
	DiffOpEqual  = "equal"
	DiffOpDelete = "delete"
	DiffOpInsert = "insert"
)

// DifferWrapper wraps go-diff functionality for Lua
type DifferWrapper struct {
	dmp *diffmatchpatch.DiffMatchPatch
}

// newDiffer creates a new differ instance
func newDiffer(l *lua.LState) int {
	// Parse options if provided
	var options *DiffOptions
	if l.GetTop() > 0 && l.Get(1).Type() == lua.LTTable {
		options = parseDiffOptions(l.CheckTable(1))
	}

	// Create the go-diff instance
	dmp := diffmatchpatch.New()

	// Apply options if provided
	if options != nil {
		if options.DiffTimeout >= 0 {
			dmp.DiffTimeout = time.Duration(options.DiffTimeout * float64(time.Second))
		}
		if options.DiffEditCost > 0 {
			dmp.DiffEditCost = int(options.DiffEditCost)
		}
		if options.MatchThreshold >= 0 {
			dmp.MatchThreshold = options.MatchThreshold
		}
		if options.MatchDistance > 0 {
			dmp.MatchDistance = options.MatchDistance
		}
		if options.PatchDeleteThreshold >= 0 {
			dmp.PatchDeleteThreshold = options.PatchDeleteThreshold
		}
		if options.PatchMargin > 0 {
			dmp.PatchMargin = options.PatchMargin
		}
	}

	// Wrap it
	wrapper := &DifferWrapper{
		dmp: dmp,
	}

	// Create userdata and set metatable
	ud := l.NewUserData()
	ud.Value = wrapper
	l.SetMetatable(ud, value.GetTypeMetatable(l, "Differ"))

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

// DiffOptions holds configuration for the differ
type DiffOptions struct {
	DiffTimeout          float64
	DiffEditCost         int16
	MatchThreshold       float64
	MatchDistance        int
	PatchDeleteThreshold float64
	PatchMargin          int
}

// parseDiffOptions parses options from a Lua table
func parseDiffOptions(table *lua.LTable) *DiffOptions {
	options := &DiffOptions{
		DiffTimeout:          -1, // Use -1 to indicate not set
		MatchThreshold:       -1,
		PatchDeleteThreshold: -1,
	}

	table.ForEach(func(key, value lua.LValue) {
		switch key.String() {
		case "diff_timeout":
			if num, ok := value.(lua.LNumber); ok {
				options.DiffTimeout = float64(num)
			}
		case "diff_edit_cost":
			if num, ok := value.(lua.LNumber); ok {
				options.DiffEditCost = int16(num)
			}
		case "match_threshold":
			if num, ok := value.(lua.LNumber); ok {
				options.MatchThreshold = float64(num)
			}
		case "match_distance":
			if num, ok := value.(lua.LNumber); ok {
				options.MatchDistance = int(num)
			}
		case "patch_delete_threshold":
			if num, ok := value.(lua.LNumber); ok {
				options.PatchDeleteThreshold = float64(num)
			}
		case "patch_margin":
			if num, ok := value.(lua.LNumber); ok {
				options.PatchMargin = int(num)
			}
		}
	})

	return options
}

// checkDiffer checks if the first argument is a valid DifferWrapper
func checkDiffer(l *lua.LState) *DifferWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*DifferWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected Differ")
	return nil
}

// registerDiffer registers the Differ type and its methods
func registerDiffer(l *lua.LState) {
	value.RegisterTypeMethods(l, "Differ", nil, map[string]lua.LGFunction{
		"compare":     differCompare,
		"pretty_text": differPrettyText,
		"patch_make":  differPatchMake,
		"patch_apply": differPatchApply,
		"summarize":   differSummarize,
	})
}

// differCompare implements the compare method
func differCompare(l *lua.LState) int {
	wrapper := checkDiffer(l)
	text1 := l.CheckString(2)
	text2 := l.CheckString(3)

	// Perform the diff
	diffs := wrapper.dmp.DiffMain(text1, text2, false)

	// Convert diffs to Lua table
	diffsTable := l.CreateTable(len(diffs), 0)
	for i, diff := range diffs {
		diffTable := l.CreateTable(0, 2)

		// Convert operation to string
		var op string
		switch diff.Type {
		case diffmatchpatch.DiffEqual:
			op = DiffOpEqual
		case diffmatchpatch.DiffDelete:
			op = DiffOpDelete
		case diffmatchpatch.DiffInsert:
			op = DiffOpInsert
		}

		diffTable.RawSetString("operation", lua.LString(op))
		diffTable.RawSetString("text", lua.LString(diff.Text))
		diffsTable.RawSetInt(i+1, diffTable)
	}

	l.Push(diffsTable)
	l.Push(lua.LNil)
	return 2
}

// differPrettyText implements the pretty_text method
func differPrettyText(l *lua.LState) int {
	wrapper := checkDiffer(l)
	diffsTable := l.CheckTable(2)

	// Convert Lua diffs back to go-diff format
	var diffs []diffmatchpatch.Diff
	diffsTable.ForEach(func(_, value lua.LValue) {
		if diffTable, ok := value.(*lua.LTable); ok {
			var diff diffmatchpatch.Diff

			operation := diffTable.RawGetString("operation")
			text := diffTable.RawGetString("text")

			switch operation.String() {
			case DiffOpEqual:
				diff.Type = diffmatchpatch.DiffEqual
			case DiffOpDelete:
				diff.Type = diffmatchpatch.DiffDelete
			case DiffOpInsert:
				diff.Type = diffmatchpatch.DiffInsert
			}
			diff.Text = text.String()
			diffs = append(diffs, diff)
		}
	})

	// Generate pretty text
	prettyText := wrapper.dmp.DiffPrettyText(diffs)

	l.Push(lua.LString(prettyText))
	l.Push(lua.LNil)
	return 2
}

// differPatchMake implements the patch_make method
func differPatchMake(l *lua.LState) int {
	wrapper := checkDiffer(l)
	text1 := l.CheckString(2)
	text2 := l.CheckString(3)

	// Create patches
	patches := wrapper.dmp.PatchMake(text1, text2)

	// Convert patches to Lua table
	patchesTable := l.CreateTable(len(patches), 0)
	for i, patch := range patches {
		patchTable := l.CreateTable(0, 1)
		patchTable.RawSetString("text", lua.LString(patch.String()))
		patchesTable.RawSetInt(i+1, patchTable)
	}

	l.Push(patchesTable)
	l.Push(lua.LNil)
	return 2
}

// differPatchApply implements the patch_apply method
func differPatchApply(l *lua.LState) int {
	wrapper := checkDiffer(l)
	patchesTable := l.CheckTable(2)
	text := l.CheckString(3)

	// Convert Lua patches back to go-diff format
	var patches []diffmatchpatch.Patch
	patchesTable.ForEach(func(_, value lua.LValue) {
		if patchTable, ok := value.(*lua.LTable); ok {
			patchText := patchTable.RawGetString("text").String()
			patchObjects, err := wrapper.dmp.PatchFromText(patchText)
			if err == nil {
				patches = append(patches, patchObjects...)
			}
		}
	})

	// Apply patches
	results, success := wrapper.dmp.PatchApply(patches, text)

	// Check if all patches applied successfully
	allSuccess := true
	for _, s := range success {
		if !s {
			allSuccess = false
			break
		}
	}

	l.Push(lua.LString(results))
	l.Push(lua.LBool(allSuccess))
	return 2
}

// differSummarize implements the summarize method
func differSummarize(l *lua.LState) int {
	checkDiffer(l)                // Validate the differ object
	diffsTable := l.CheckTable(2) // Get diffs table from second argument

	var insertions, deletions, equals int

	diffsTable.ForEach(func(_, value lua.LValue) {
		if diffTable, ok := value.(*lua.LTable); ok {
			operation := diffTable.RawGetString("operation").String()
			text := diffTable.RawGetString("text").String()
			length := len(text)

			switch operation {
			case DiffOpEqual:
				equals += length
			case DiffOpDelete:
				deletions += length
			case DiffOpInsert:
				insertions += length
			}
		}
	})

	// Create summary table
	summary := l.CreateTable(0, 3)
	summary.RawSetString("insertions", lua.LNumber(insertions))
	summary.RawSetString("deletions", lua.LNumber(deletions))
	summary.RawSetString("equals", lua.LNumber(equals))

	l.Push(summary)
	return 1
}
