package text

import (
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

const (
	regexpMetatable = "text.Regexp"
	differMetatable = "text.Differ"
)

const (
	DiffOpEqual  = "equal"
	DiffOpDelete = "delete"
	DiffOpInsert = "insert"
)

var (
	moduleTable  *lua.LTable
	regexpMT     *lua.LTable
	differMT     *lua.LTable
	registration *luaapi.Registration
	initOnce     sync.Once
)

// Module is the singleton text module instance.
var Module = &textModule{}

type textModule struct{}

func (m *textModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "text",
		Description: "Text processing: regex, diff, and patch operations",
		Class:       []string{luaapi.ClassDeterministic},
	}
}

func (m *textModule) Register(l *lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		regexpMT = createRegexpMetatable(l)
		differMT = createDifferMetatable(l)

		mod := &lua.LTable{}

		regexpMod := &lua.LTable{}
		regexpMod.RawSetString("compile", lua.LGoFunc(luaRegexpCompile))
		regexpMod.Immutable = true
		mod.RawSetString("regexp", regexpMod)

		diffMod := &lua.LTable{}
		diffMod.RawSetString("new", lua.LGoFunc(luaDiffNew))
		diffMod.Immutable = true
		mod.RawSetString("diff", diffMod)

		mod.Immutable = true
		moduleTable = mod

		registration = &luaapi.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})

	l.SetField(l.Get(lua.RegistryIndex), regexpMetatable, regexpMT)
	l.SetField(l.Get(lua.RegistryIndex), differMetatable, differMT)

	return registration
}

func (m *textModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use luaapi.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	luaapi.LoadModule(l, Module)
}

type RegexpWrapper struct {
	regexp *regexp.Regexp
}

type DifferWrapper struct {
	dmp *diffmatchpatch.DiffMatchPatch
}

type DiffOptions struct {
	DiffTimeout          float64
	DiffEditCost         int16
	MatchThreshold       float64
	MatchDistance        int
	PatchDeleteThreshold float64
	PatchMargin          int
}

func getRegexpMT(l *lua.LState) lua.LValue {
	return l.GetField(l.Get(lua.RegistryIndex), regexpMetatable)
}

func getDifferMT(l *lua.LState) lua.LValue {
	return l.GetField(l.Get(lua.RegistryIndex), differMetatable)
}

func createRegexpMetatable(l *lua.LState) *lua.LTable {
	mt := l.CreateTable(0, 2)

	index := l.CreateTable(0, 12)
	index.RawSetString("find_all_string_submatch", lua.LGoFunc(regexpFindAllStringSubmatch))
	index.RawSetString("find_string_submatch", lua.LGoFunc(regexpFindStringSubmatch))
	index.RawSetString("find_all_string", lua.LGoFunc(regexpFindAllString))
	index.RawSetString("find_string", lua.LGoFunc(regexpFindString))
	index.RawSetString("find_all_string_index", lua.LGoFunc(regexpFindAllStringIndex))
	index.RawSetString("find_string_index", lua.LGoFunc(regexpFindStringIndex))
	index.RawSetString("replace_all_string", lua.LGoFunc(regexpReplaceAllString))
	index.RawSetString("match_string", lua.LGoFunc(regexpMatchString))
	index.RawSetString("split", lua.LGoFunc(regexpSplit))
	index.RawSetString("num_subexp", lua.LGoFunc(regexpNumSubexp))
	index.RawSetString("subexp_names", lua.LGoFunc(regexpSubexpNames))
	index.RawSetString("string", lua.LGoFunc(regexpString))
	index.Immutable = true

	mt.RawSetString("__index", index)
	mt.Immutable = true
	return mt
}

func createDifferMetatable(l *lua.LState) *lua.LTable {
	mt := l.CreateTable(0, 2)

	index := l.CreateTable(0, 6)
	index.RawSetString("compare", lua.LGoFunc(differCompare))
	index.RawSetString("pretty_text", lua.LGoFunc(differPrettyText))
	index.RawSetString("pretty_html", lua.LGoFunc(differPrettyHTML))
	index.RawSetString("patch_make", lua.LGoFunc(differPatchMake))
	index.RawSetString("patch_apply", lua.LGoFunc(differPatchApply))
	index.RawSetString("summarize", lua.LGoFunc(differSummarize))
	index.Immutable = true

	mt.RawSetString("__index", index)
	mt.Immutable = true
	return mt
}

func checkRegexp(l *lua.LState) *RegexpWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*RegexpWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected Regexp")
	return nil
}

func checkDiffer(l *lua.LState) *DifferWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*DifferWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected Differ")
	return nil
}

func luaRegexpCompile(l *lua.LState) int {
	pattern := l.CheckString(1)

	re, err := regexp.Compile(pattern)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("regex compile error: %v", err)))
		return 2
	}

	wrapper := &RegexpWrapper{regexp: re}
	ud := l.NewUserData()
	ud.Value = wrapper
	ud.Metatable = getRegexpMT(l)

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

func luaDiffNew(l *lua.LState) int {
	var options *DiffOptions
	if l.GetTop() > 0 && l.Get(1).Type() == lua.LTTable {
		options = parseDiffOptions(l.CheckTable(1))
	}

	dmp := diffmatchpatch.New()

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

	wrapper := &DifferWrapper{dmp: dmp}
	ud := l.NewUserData()
	ud.Value = wrapper
	ud.Metatable = getDifferMT(l)

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

func parseDiffOptions(table *lua.LTable) *DiffOptions {
	options := &DiffOptions{
		DiffTimeout:          -1,
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

func regexpFindAllStringSubmatch(l *lua.LState) int {
	wrapper := checkRegexp(l)
	if wrapper == nil {
		return 0
	}
	content := l.CheckString(2)
	matches := wrapper.regexp.FindAllStringSubmatch(content, -1)

	result := l.CreateTable(len(matches), 0)
	for i, match := range matches {
		matchTable := l.CreateTable(len(match), 0)
		for j, submatch := range match {
			matchTable.RawSetInt(j+1, lua.LString(submatch))
		}
		result.RawSetInt(i+1, matchTable)
	}
	l.Push(result)
	return 1
}

func regexpFindStringSubmatch(l *lua.LState) int {
	wrapper := checkRegexp(l)
	if wrapper == nil {
		return 0
	}
	content := l.CheckString(2)
	match := wrapper.regexp.FindStringSubmatch(content)

	if match == nil {
		l.Push(lua.LNil)
		return 1
	}

	result := l.CreateTable(len(match), 0)
	for i, submatch := range match {
		result.RawSetInt(i+1, lua.LString(submatch))
	}
	l.Push(result)
	return 1
}

func regexpFindAllString(l *lua.LState) int {
	wrapper := checkRegexp(l)
	if wrapper == nil {
		return 0
	}
	content := l.CheckString(2)
	matches := wrapper.regexp.FindAllString(content, -1)

	result := l.CreateTable(len(matches), 0)
	for i, match := range matches {
		result.RawSetInt(i+1, lua.LString(match))
	}
	l.Push(result)
	return 1
}

func regexpFindString(l *lua.LState) int {
	wrapper := checkRegexp(l)
	if wrapper == nil {
		return 0
	}
	content := l.CheckString(2)
	match := wrapper.regexp.FindString(content)

	if match == "" {
		l.Push(lua.LNil)
	} else {
		l.Push(lua.LString(match))
	}
	return 1
}

func regexpFindAllStringIndex(l *lua.LState) int {
	wrapper := checkRegexp(l)
	if wrapper == nil {
		return 0
	}
	content := l.CheckString(2)
	indices := wrapper.regexp.FindAllStringIndex(content, -1)

	if indices == nil {
		l.Push(lua.LNil)
		return 1
	}

	result := l.CreateTable(len(indices), 0)
	for i, index := range indices {
		indexTable := l.CreateTable(2, 0)
		indexTable.RawSetInt(1, lua.LNumber(index[0]+1))
		indexTable.RawSetInt(2, lua.LNumber(index[1]))
		result.RawSetInt(i+1, indexTable)
	}
	l.Push(result)
	return 1
}

func regexpFindStringIndex(l *lua.LState) int {
	wrapper := checkRegexp(l)
	if wrapper == nil {
		return 0
	}
	content := l.CheckString(2)
	index := wrapper.regexp.FindStringIndex(content)

	if index == nil {
		l.Push(lua.LNil)
		return 1
	}

	result := l.CreateTable(2, 0)
	result.RawSetInt(1, lua.LNumber(index[0]+1))
	result.RawSetInt(2, lua.LNumber(index[1]))
	l.Push(result)
	return 1
}

func regexpReplaceAllString(l *lua.LState) int {
	wrapper := checkRegexp(l)
	if wrapper == nil {
		return 0
	}
	content := l.CheckString(2)
	replacement := l.CheckString(3)
	result := wrapper.regexp.ReplaceAllString(content, replacement)
	l.Push(lua.LString(result))
	return 1
}

func regexpMatchString(l *lua.LState) int {
	wrapper := checkRegexp(l)
	if wrapper == nil {
		return 0
	}
	content := l.CheckString(2)
	matches := wrapper.regexp.MatchString(content)
	l.Push(lua.LBool(matches))
	return 1
}

func regexpSplit(l *lua.LState) int {
	wrapper := checkRegexp(l)
	if wrapper == nil {
		return 0
	}
	content := l.CheckString(2)
	n := int(l.OptNumber(3, -1))
	parts := wrapper.regexp.Split(content, n)

	result := l.CreateTable(len(parts), 0)
	for i, part := range parts {
		result.RawSetInt(i+1, lua.LString(part))
	}
	l.Push(result)
	return 1
}

func regexpNumSubexp(l *lua.LState) int {
	wrapper := checkRegexp(l)
	if wrapper == nil {
		return 0
	}
	l.Push(lua.LNumber(wrapper.regexp.NumSubexp()))
	return 1
}

func regexpSubexpNames(l *lua.LState) int {
	wrapper := checkRegexp(l)
	if wrapper == nil {
		return 0
	}
	names := wrapper.regexp.SubexpNames()

	result := l.CreateTable(len(names), 0)
	for i, name := range names {
		result.RawSetInt(i+1, lua.LString(name))
	}
	l.Push(result)
	return 1
}

func regexpString(l *lua.LState) int {
	wrapper := checkRegexp(l)
	if wrapper == nil {
		return 0
	}
	l.Push(lua.LString(wrapper.regexp.String()))
	return 1
}

func differCompare(l *lua.LState) int {
	wrapper := checkDiffer(l)
	if wrapper == nil {
		return 0
	}
	text1 := l.CheckString(2)
	text2 := l.CheckString(3)

	diffs := wrapper.dmp.DiffMain(text1, text2, false)

	diffsTable := l.CreateTable(len(diffs), 0)
	for i, diff := range diffs {
		diffTable := l.CreateTable(0, 2)

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

func differPrettyText(l *lua.LState) int {
	wrapper := checkDiffer(l)
	if wrapper == nil {
		return 0
	}
	diffsTable := l.CheckTable(2)

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

	prettyText := wrapper.dmp.DiffPrettyText(diffs)

	l.Push(lua.LString(prettyText))
	l.Push(lua.LNil)
	return 2
}

func differPrettyHTML(l *lua.LState) int {
	wrapper := checkDiffer(l)
	if wrapper == nil {
		return 0
	}
	diffsTable := l.CheckTable(2)

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

	prettyHTML := wrapper.dmp.DiffPrettyHtml(diffs)

	l.Push(lua.LString(prettyHTML))
	l.Push(lua.LNil)
	return 2
}

func differPatchMake(l *lua.LState) int {
	wrapper := checkDiffer(l)
	if wrapper == nil {
		return 0
	}
	text1 := l.CheckString(2)
	text2 := l.CheckString(3)

	patches := wrapper.dmp.PatchMake(text1, text2)

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

func differPatchApply(l *lua.LState) int {
	wrapper := checkDiffer(l)
	if wrapper == nil {
		return 0
	}
	patchesTable := l.CheckTable(2)
	text := l.CheckString(3)

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

	results, success := wrapper.dmp.PatchApply(patches, text)

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

func differSummarize(l *lua.LState) int {
	if checkDiffer(l) == nil {
		return 0
	}
	diffsTable := l.CheckTable(2)

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

	summary := l.CreateTable(0, 3)
	summary.RawSetString("insertions", lua.LNumber(insertions))
	summary.RawSetString("deletions", lua.LNumber(deletions))
	summary.RawSetString("equals", lua.LNumber(equals))

	l.Push(summary)
	return 1
}
