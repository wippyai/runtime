package text

import (
	"regexp"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/tmc/langchaingo/textsplitter"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const (
	typeRegexp   = "text.Regexp"
	typeDiffer   = "text.Differ"
	typeSplitter = "text.Splitter"
)

const (
	DiffOpEqual  = "equal"
	DiffOpDelete = "delete"
	DiffOpInsert = "insert"
)

// Module is the text module definition.
var Module = &luaapi.ModuleDef{
	Name:        "text",
	Description: "Text processing: regex, diff, and patch operations",
	Class:       []string{luaapi.ClassDeterministic},
	Build:       buildModule,
}

func init() {
	// Register type metatables once at startup
	value.RegisterTypeMethods(nil, typeRegexp, nil, map[string]lua.LGoFunc{
		"find_all_string_submatch": regexpFindAllStringSubmatch,
		"find_string_submatch":     regexpFindStringSubmatch,
		"find_all_string":          regexpFindAllString,
		"find_string":              regexpFindString,
		"find_all_string_index":    regexpFindAllStringIndex,
		"find_string_index":        regexpFindStringIndex,
		"replace_all_string":       regexpReplaceAllString,
		"match_string":             regexpMatchString,
		"split":                    regexpSplit,
		"num_subexp":               regexpNumSubexp,
		"subexp_names":             regexpSubexpNames,
		"string":                   regexpString,
	})

	value.RegisterTypeMethods(nil, typeDiffer, nil, map[string]lua.LGoFunc{
		"compare":     differCompare,
		"pretty_text": differPrettyText,
		"pretty_html": differPrettyHTML,
		"patch_make":  differPatchMake,
		"patch_apply": differPatchApply,
		"summarize":   differSummarize,
	})

	value.RegisterTypeMethods(nil, typeSplitter, nil, map[string]lua.LGoFunc{
		"split_text":  splitterSplitText,
		"split_batch": splitterSplitBatch,
	})
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 3)

	regexpMod := lua.CreateTable(0, 1)
	regexpMod.RawSetString("compile", lua.LGoFunc(luaRegexpCompile))
	regexpMod.Immutable = true
	mod.RawSetString("regexp", regexpMod)

	diffMod := lua.CreateTable(0, 1)
	diffMod.RawSetString("new", lua.LGoFunc(luaDiffNew))
	diffMod.Immutable = true
	mod.RawSetString("diff", diffMod)

	splitterMod := lua.CreateTable(0, 2)
	splitterMod.RawSetString("recursive", lua.LGoFunc(luaSplitterRecursive))
	splitterMod.RawSetString("markdown", lua.LGoFunc(luaSplitterMarkdown))
	splitterMod.Immutable = true
	mod.RawSetString("splitter", splitterMod)

	mod.Immutable = true
	return mod, nil
}

// Regexp wraps a compiled regular expression.
type Regexp struct {
	re *regexp.Regexp
}

// Differ wraps a diff-match-patch instance.
type Differ struct {
	dmp *diffmatchpatch.DiffMatchPatch
}

// DiffOptions configures diff behavior.
type DiffOptions struct {
	DiffTimeout          float64
	DiffEditCost         int16
	MatchThreshold       float64
	MatchDistance        int
	PatchDeleteThreshold float64
	PatchMargin          int
}

func checkRegexp(l *lua.LState) *Regexp {
	ud := l.CheckUserData(1)
	if r, ok := ud.Value.(*Regexp); ok {
		return r
	}
	l.ArgError(1, "expected text.Regexp")
	return nil
}

func checkDiffer(l *lua.LState) *Differ {
	ud := l.CheckUserData(1)
	if d, ok := ud.Value.(*Differ); ok {
		return d
	}
	l.ArgError(1, "expected text.Differ")
	return nil
}

func luaRegexpCompile(l *lua.LState) int {
	pattern := l.CheckString(1)

	re, err := regexp.Compile(pattern)
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "regex compile error").
			WithKind(lua.KindInvalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	value.PushTypedUserData(l, &Regexp{re: re}, typeRegexp)
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

	value.PushTypedUserData(l, &Differ{dmp: dmp}, typeDiffer)
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
	matches := wrapper.re.FindAllStringSubmatch(content, -1)

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
	match := wrapper.re.FindStringSubmatch(content)

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
	matches := wrapper.re.FindAllString(content, -1)

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
	match := wrapper.re.FindString(content)

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
	indices := wrapper.re.FindAllStringIndex(content, -1)

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
	index := wrapper.re.FindStringIndex(content)

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
	result := wrapper.re.ReplaceAllString(content, replacement)
	l.Push(lua.LString(result))
	return 1
}

func regexpMatchString(l *lua.LState) int {
	wrapper := checkRegexp(l)
	if wrapper == nil {
		return 0
	}
	content := l.CheckString(2)
	matches := wrapper.re.MatchString(content)
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
	parts := wrapper.re.Split(content, n)

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
	l.Push(lua.LNumber(wrapper.re.NumSubexp()))
	return 1
}

func regexpSubexpNames(l *lua.LState) int {
	wrapper := checkRegexp(l)
	if wrapper == nil {
		return 0
	}
	names := wrapper.re.SubexpNames()

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
	l.Push(lua.LString(wrapper.re.String()))
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

// Splitter wraps a text splitter.
type Splitter struct {
	splitter textsplitter.TextSplitter
}

func checkSplitter(l *lua.LState) *Splitter {
	ud := l.CheckUserData(1)
	if s, ok := ud.Value.(*Splitter); ok {
		return s
	}
	l.ArgError(1, "expected text.Splitter")
	return nil
}

func luaSplitterRecursive(l *lua.LState) int {
	var options []textsplitter.Option

	if l.GetTop() > 0 && l.Get(1).Type() == lua.LTTable {
		options = parseRecursiveOptions(l.CheckTable(1))
	}

	splitter := textsplitter.NewRecursiveCharacter(options...)
	value.PushTypedUserData(l, &Splitter{splitter: splitter}, typeSplitter)
	l.Push(lua.LNil)
	return 2
}

func luaSplitterMarkdown(l *lua.LState) int {
	var options []textsplitter.Option

	if l.GetTop() > 0 && l.Get(1).Type() == lua.LTTable {
		options = parseMarkdownOptions(l.CheckTable(1))
	}

	splitter := textsplitter.NewMarkdownTextSplitter(options...)
	value.PushTypedUserData(l, &Splitter{splitter: splitter}, typeSplitter)
	l.Push(lua.LNil)
	return 2
}

func parseRecursiveOptions(table *lua.LTable) []textsplitter.Option {
	var options []textsplitter.Option

	table.ForEach(func(key, val lua.LValue) {
		switch key.String() {
		case "chunk_size":
			if num, ok := val.(lua.LNumber); ok {
				options = append(options, textsplitter.WithChunkSize(int(num)))
			}
		case "chunk_overlap":
			if num, ok := val.(lua.LNumber); ok {
				options = append(options, textsplitter.WithChunkOverlap(int(num)))
			}
		case "keep_separator":
			if b, ok := val.(lua.LBool); ok {
				options = append(options, textsplitter.WithKeepSeparator(bool(b)))
			}
		case "separators":
			if tbl, ok := val.(*lua.LTable); ok {
				separators := parseSeparatorsTable(tbl)
				if len(separators) > 0 {
					options = append(options, textsplitter.WithSeparators(separators))
				}
			}
		}
	})

	return options
}

func parseMarkdownOptions(table *lua.LTable) []textsplitter.Option {
	var options []textsplitter.Option

	table.ForEach(func(key, val lua.LValue) {
		switch key.String() {
		case "chunk_size":
			if num, ok := val.(lua.LNumber); ok {
				options = append(options, textsplitter.WithChunkSize(int(num)))
			}
		case "chunk_overlap":
			if num, ok := val.(lua.LNumber); ok {
				options = append(options, textsplitter.WithChunkOverlap(int(num)))
			}
		case "code_blocks":
			if b, ok := val.(lua.LBool); ok {
				options = append(options, textsplitter.WithCodeBlocks(bool(b)))
			}
		case "reference_links":
			if b, ok := val.(lua.LBool); ok {
				options = append(options, textsplitter.WithReferenceLinks(bool(b)))
			}
		case "heading_hierarchy":
			if b, ok := val.(lua.LBool); ok {
				options = append(options, textsplitter.WithHeadingHierarchy(bool(b)))
			}
		case "join_table_rows":
			if b, ok := val.(lua.LBool); ok {
				options = append(options, textsplitter.WithJoinTableRows(bool(b)))
			}
		case "separators":
			if tbl, ok := val.(*lua.LTable); ok {
				separators := parseSeparatorsTable(tbl)
				if len(separators) > 0 {
					options = append(options, textsplitter.WithSeparators(separators))
				}
			}
		}
	})

	return options
}

func parseSeparatorsTable(table *lua.LTable) []string {
	var separators []string
	table.ForEach(func(_, val lua.LValue) {
		if str, ok := val.(lua.LString); ok {
			separators = append(separators, string(str))
		}
	})
	return separators
}

func splitterSplitText(l *lua.LState) int {
	wrapper := checkSplitter(l)
	if wrapper == nil {
		return 0
	}
	text := l.CheckString(2)

	chunks, err := wrapper.splitter.SplitText(text)
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "split_text").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	chunksTable := l.CreateTable(len(chunks), 0)
	for i, chunk := range chunks {
		chunksTable.RawSetInt(i+1, lua.LString(chunk))
	}

	l.Push(chunksTable)
	l.Push(lua.LNil)
	return 2
}

func splitterSplitBatch(l *lua.LState) int {
	wrapper := checkSplitter(l)
	if wrapper == nil {
		return 0
	}
	pagesTable := l.CheckTable(2)

	var allChunks []lua.LValue

	pagesTable.ForEach(func(_, val lua.LValue) {
		pageTable, ok := val.(*lua.LTable)
		if !ok {
			return
		}

		var content string
		var metaTable *lua.LTable

		pageTable.ForEach(func(key, v lua.LValue) {
			switch key.String() {
			case "content":
				if str, ok := v.(lua.LString); ok {
					content = string(str)
				}
			case "metadata":
				if mt, ok := v.(*lua.LTable); ok {
					metaTable = mt
				}
			}
		})

		if content == "" {
			return
		}

		chunks, err := wrapper.splitter.SplitText(content)
		if err != nil {
			return
		}

		for _, chunkText := range chunks {
			chunkTable := l.CreateTable(0, 2)
			chunkTable.RawSetString("content", lua.LString(chunkText))

			if metaTable != nil {
				chunkTable.RawSetString("metadata", metaTable)
			} else {
				chunkTable.RawSetString("metadata", l.CreateTable(0, 0))
			}

			allChunks = append(allChunks, chunkTable)
		}
	})

	result := l.CreateTable(len(allChunks), 0)
	for i, chunk := range allChunks {
		result.RawSetInt(i+1, chunk)
	}

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}
