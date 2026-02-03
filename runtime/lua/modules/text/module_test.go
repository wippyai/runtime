package text

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	mod := l.GetGlobal("text")
	if mod.Type() != lua.LTTable {
		t.Fatal("text module not registered")
	}

	modTbl := mod.(*lua.LTable)

	regexpMod := modTbl.RawGetString("regexp")
	if regexpMod.Type() != lua.LTTable {
		t.Error("regexp submodule not registered")
	}

	diffMod := modTbl.RawGetString("diff")
	if diffMod.Type() != lua.LTTable {
		t.Error("diff submodule not registered")
	}
}

func TestRegexpCompile(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local re, err = text.regexp.compile("[a-z]+")
		if err then
			error("unexpected error: " .. err)
		end
		if re == nil then
			error("regexp should not be nil")
		end
	`)
	if err != nil {
		t.Errorf("regexp compile failed: %v", err)
	}
}

func TestRegexpCompileInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local re, err = text.regexp.compile("[invalid")
		if re ~= nil then
			error("expected nil for invalid pattern")
		end
		if err == nil then
			error("expected error for invalid pattern")
		end
		if err:kind() ~= "Invalid" then
			error("expected Invalid kind, got " .. tostring(err:kind()))
		end
		if err:retryable() ~= false then
			error("expected not retryable")
		end
	`)
	if err != nil {
		t.Errorf("regexp compile invalid failed: %v", err)
	}
}

func TestRegexpMatchString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local re, _ = text.regexp.compile("[0-9]+")
		local matches = re:match_string("hello123world")
		if matches ~= true then
			error("expected true, got " .. tostring(matches))
		end

		matches = re:match_string("helloworld")
		if matches ~= false then
			error("expected false, got " .. tostring(matches))
		end
	`)
	if err != nil {
		t.Errorf("regexp match_string failed: %v", err)
	}
}

func TestRegexpFindString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local re, _ = text.regexp.compile("[0-9]+")
		local match = re:find_string("hello123world456")
		if match ~= "123" then
			error("expected '123', got '" .. tostring(match) .. "'")
		end

		match = re:find_string("helloworld")
		if match ~= nil then
			error("expected nil, got '" .. tostring(match) .. "'")
		end
	`)
	if err != nil {
		t.Errorf("regexp find_string failed: %v", err)
	}
}

func TestRegexpFindAllString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local re, _ = text.regexp.compile("[0-9]+")
		local matches = re:find_all_string("hello123world456end789")
		if #matches ~= 3 then
			error("expected 3 matches, got " .. #matches)
		end
		if matches[1] ~= "123" then
			error("first match should be '123'")
		end
		if matches[2] ~= "456" then
			error("second match should be '456'")
		end
		if matches[3] ~= "789" then
			error("third match should be '789'")
		end
	`)
	if err != nil {
		t.Errorf("regexp find_all_string failed: %v", err)
	}
}

func TestRegexpFindStringSubmatch(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local re, _ = text.regexp.compile("([a-z]+)([0-9]+)")
		local match = re:find_string_submatch("hello123world")
		if match == nil then
			error("expected match")
		end
		if #match ~= 3 then
			error("expected 3 elements, got " .. #match)
		end
		if match[1] ~= "hello123" then
			error("full match should be 'hello123'")
		end
		if match[2] ~= "hello" then
			error("first group should be 'hello'")
		end
		if match[3] ~= "123" then
			error("second group should be '123'")
		end
	`)
	if err != nil {
		t.Errorf("regexp find_string_submatch failed: %v", err)
	}
}

func TestRegexpFindAllStringSubmatch(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local re, _ = text.regexp.compile('([a-z]+)@([a-z]+)')
		local matches = re:find_all_string_submatch("user@example and admin@test")
		if #matches ~= 2 then
			error("expected 2 matches, got " .. #matches)
		end
		if matches[1][1] ~= "user@example" then
			error("first full match wrong")
		end
		if matches[1][2] ~= "user" then
			error("first group 1 wrong")
		end
		if matches[1][3] ~= "example" then
			error("first group 2 wrong")
		end
	`)
	if err != nil {
		t.Errorf("regexp find_all_string_submatch failed: %v", err)
	}
}

func TestRegexpFindStringIndex(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local re, _ = text.regexp.compile("page\\d+")
		local content = "The page1 and page2 are here"
		local index = re:find_string_index(content)
		if index == nil then
			error("expected index")
		end
		if index[1] ~= 5 then
			error("expected start 5, got " .. index[1])
		end
		if index[2] ~= 9 then
			error("expected end 9, got " .. index[2])
		end
		local extracted = content:sub(index[1], index[2])
		if extracted ~= "page1" then
			error("expected 'page1', got '" .. extracted .. "'")
		end
	`)
	if err != nil {
		t.Errorf("regexp find_string_index failed: %v", err)
	}
}

func TestRegexpFindAllStringIndex(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local re, _ = text.regexp.compile("page\\d+")
		local content = "The page1 and page2 are here"
		local indices = re:find_all_string_index(content)
		if indices == nil then
			error("expected indices")
		end
		if #indices ~= 2 then
			error("expected 2 indices, got " .. #indices)
		end
	`)
	if err != nil {
		t.Errorf("regexp find_all_string_index failed: %v", err)
	}
}

func TestRegexpReplaceAllString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local re, _ = text.regexp.compile("[0-9]+")
		local result = re:replace_all_string("hello123world456", "XXX")
		if result ~= "helloXXXworldXXX" then
			error("expected 'helloXXXworldXXX', got '" .. result .. "'")
		end
	`)
	if err != nil {
		t.Errorf("regexp replace_all_string failed: %v", err)
	}
}

func TestRegexpSplit(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local re, _ = text.regexp.compile(",")
		local parts = re:split("apple,banana,cherry", -1)
		if #parts ~= 3 then
			error("expected 3 parts, got " .. #parts)
		end
		if parts[1] ~= "apple" then
			error("first part should be 'apple'")
		end
		if parts[2] ~= "banana" then
			error("second part should be 'banana'")
		end
		if parts[3] ~= "cherry" then
			error("third part should be 'cherry'")
		end

		local limited = re:split("apple,banana,cherry,date", 2)
		if #limited ~= 2 then
			error("expected 2 parts with limit, got " .. #limited)
		end
		if limited[2] ~= "banana,cherry,date" then
			error("second part should be 'banana,cherry,date'")
		end
	`)
	if err != nil {
		t.Errorf("regexp split failed: %v", err)
	}
}

func TestRegexpNumSubexp(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local re1, _ = text.regexp.compile("([a-z]+)([0-9]+)")
		if re1:num_subexp() ~= 2 then
			error("expected 2 subexpressions")
		end

		local re2, _ = text.regexp.compile("[a-z]+")
		if re2:num_subexp() ~= 0 then
			error("expected 0 subexpressions")
		end
	`)
	if err != nil {
		t.Errorf("regexp num_subexp failed: %v", err)
	}
}

func TestRegexpSubexpNames(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local re, _ = text.regexp.compile("(?P<name>[a-z]+)(?P<num>[0-9]+)")
		local names = re:subexp_names()
		if #names ~= 3 then
			error("expected 3 names, got " .. #names)
		end
		if names[1] ~= "" then
			error("first should be empty (full match)")
		end
		if names[2] ~= "name" then
			error("second should be 'name'")
		end
		if names[3] ~= "num" then
			error("third should be 'num'")
		end
	`)
	if err != nil {
		t.Errorf("regexp subexp_names failed: %v", err)
	}
}

func TestRegexpString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local re, _ = text.regexp.compile("[a-z]+")
		local str = re:string()
		if str ~= "[a-z]+" then
			error("expected '[a-z]+', got '" .. str .. "'")
		end
	`)
	if err != nil {
		t.Errorf("regexp string failed: %v", err)
	}
}

func TestDiffNew(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local diff, err = text.diff.new()
		if err then
			error("unexpected error: " .. err)
		end
		if diff == nil then
			error("diff should not be nil")
		end
	`)
	if err != nil {
		t.Errorf("diff new failed: %v", err)
	}
}

func TestDiffNewWithOptions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local diff, err = text.diff.new({
			diff_timeout = 1.0,
			match_threshold = 0.5
		})
		if err then
			error("unexpected error: " .. err)
		end
		if diff == nil then
			error("diff should not be nil")
		end
	`)
	if err != nil {
		t.Errorf("diff new with options failed: %v", err)
	}
}

func TestDiffCompare(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local diff, _ = text.diff.new()
		local diffs, err = diff:compare("hello world", "hello there")
		if err then
			error("unexpected error: " .. err)
		end
		if #diffs == 0 then
			error("expected some diffs")
		end

		local hasEqual = false
		local hasDelete = false
		local hasInsert = false
		for _, d in ipairs(diffs) do
			if d.operation == "equal" then hasEqual = true end
			if d.operation == "delete" then hasDelete = true end
			if d.operation == "insert" then hasInsert = true end
		end
		if not hasEqual then
			error("should have equal operation")
		end
		if not hasDelete then
			error("should have delete operation")
		end
		if not hasInsert then
			error("should have insert operation")
		end
	`)
	if err != nil {
		t.Errorf("diff compare failed: %v", err)
	}
}

func TestDiffCompareIdentical(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local diff, _ = text.diff.new()
		local diffs, _ = diff:compare("same text", "same text")
		if #diffs ~= 1 then
			error("expected 1 diff for identical texts, got " .. #diffs)
		end
		if diffs[1].operation ~= "equal" then
			error("expected equal operation")
		end
		if diffs[1].text ~= "same text" then
			error("expected 'same text'")
		end
	`)
	if err != nil {
		t.Errorf("diff compare identical failed: %v", err)
	}
}

func TestDiffPrettyText(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local diff, _ = text.diff.new()
		local diffs, _ = diff:compare("hello", "hallo")
		local pretty, err = diff:pretty_text(diffs)
		if err then
			error("unexpected error: " .. err)
		end
		if pretty == nil or pretty == "" then
			error("expected pretty text output")
		end
	`)
	if err != nil {
		t.Errorf("diff pretty_text failed: %v", err)
	}
}

func TestDiffPrettyHTML(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local diff, _ = text.diff.new()
		local diffs, _ = diff:compare("hello", "hallo")
		local html, err = diff:pretty_html(diffs)
		if err then
			error("unexpected error: " .. err)
		end
		if html == nil or html == "" then
			error("expected HTML output")
		end
	`)
	if err != nil {
		t.Errorf("diff pretty_html failed: %v", err)
	}
}

func TestDiffPatchMakeAndApply(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local diff, _ = text.diff.new()
		local text1 = "The quick brown fox jumps over the lazy dog"
		local text2 = "The quick red fox jumps over the lazy cat"

		local patches, err = diff:patch_make(text1, text2)
		if err then
			error("patch_make error: " .. err)
		end
		if #patches == 0 then
			error("expected some patches")
		end

		local result, success = diff:patch_apply(patches, text1)
		if not success then
			error("patch apply failed")
		end
		if result ~= text2 then
			error("patched text should equal text2")
		end
	`)
	if err != nil {
		t.Errorf("diff patch_make and patch_apply failed: %v", err)
	}
}

func TestDiffSummarize(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local diff, _ = text.diff.new()
		local diffs, _ = diff:compare("hello world", "hello there")
		local summary = diff:summarize(diffs)
		if summary == nil then
			error("expected summary")
		end
		if type(summary.insertions) ~= "number" then
			error("insertions should be a number")
		end
		if type(summary.deletions) ~= "number" then
			error("deletions should be a number")
		end
		if type(summary.equals) ~= "number" then
			error("equals should be a number")
		end
	`)
	if err != nil {
		t.Errorf("diff summarize failed: %v", err)
	}
}

func TestDocumentPageExtraction(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local content = [[
<document-page number="1">First page content</document-page>
<document-page number="2">Second page content</document-page>
<document-page number="3">Third page content</document-page>
		]]

		local re, err = text.regexp.compile('<document-page number="(\\d+)">(.*?)</document-page>')
		if err then
			error("Failed to compile regex: " .. err)
		end

		local matches = re:find_all_string_submatch(content)
		if #matches ~= 3 then
			error("Expected 3 matches, got " .. #matches)
		end

		if matches[1][2] ~= "1" then
			error("Expected page '1', got '" .. (matches[1][2] or "nil") .. "'")
		end
		if matches[1][3] ~= "First page content" then
			error("Wrong content for page 1")
		end

		if matches[2][2] ~= "2" then
			error("Expected page '2', got '" .. (matches[2][2] or "nil") .. "'")
		end
		if matches[2][3] ~= "Second page content" then
			error("Wrong content for page 2")
		end
	`)
	if err != nil {
		t.Errorf("document page extraction failed: %v", err)
	}
}

func TestTagStripping(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local html_content = "<h1>Title</h1><p>Some <b>bold</b> text.</p><div>More content</div>"

		local tag_re, err = text.regexp.compile('<[^>]*>')
		if err then
			error("Failed to compile tag regex")
		end

		local clean_text = tag_re:replace_all_string(html_content, '')
		if clean_text ~= "TitleSome bold text.More content" then
			error("Tag stripping failed: " .. clean_text)
		end
	`)
	if err != nil {
		t.Errorf("tag stripping failed: %v", err)
	}
}

func TestEmailExtraction(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local content = "Email: user@example.com and another: admin@test.org"

		local email_re, err = text.regexp.compile('([a-zA-Z0-9._%+-]+)@([a-zA-Z0-9.-]+\\.[a-zA-Z]{2,})')
		if err then
			error("Failed to compile email regex")
		end

		local first_match = email_re:find_string_submatch(content)
		if first_match == nil then
			error("Should find first email")
		end
		if first_match[1] ~= "user@example.com" then
			error("Wrong full match: " .. (first_match[1] or "nil"))
		end
		if first_match[2] ~= "user" then
			error("Wrong username: " .. (first_match[2] or "nil"))
		end
		if first_match[3] ~= "example.com" then
			error("Wrong domain: " .. (first_match[3] or "nil"))
		end

		local all_emails = email_re:find_all_string(content)
		if #all_emails ~= 2 then
			error("Expected 2 emails, got " .. #all_emails)
		end
		if all_emails[1] ~= "user@example.com" then
			error("Wrong first email")
		end
		if all_emails[2] ~= "admin@test.org" then
			error("Wrong second email")
		end
	`)
	if err != nil {
		t.Errorf("email extraction failed: %v", err)
	}
}

func TestImmutability(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local success = pcall(function()
			text.foo = "bar"
		end)
	`)
	if err != nil {
		t.Errorf("immutability test failed: %v", err)
	}
}

func TestLoadReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	tbl, _ := Module.Build()
	l1.SetGlobal(Module.Name, tbl)
	l2.SetGlobal(Module.Name, tbl)

	mod1 := l1.GetGlobal("text").(*lua.LTable)
	mod2 := l2.GetGlobal("text").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestSplitterRecursive(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local splitter, err = text.splitter.recursive({chunk_size = 100, chunk_overlap = 20})
		if err then
			error("unexpected error: " .. tostring(err))
		end
		if splitter == nil then
			error("splitter should not be nil")
		end
	`)
	if err != nil {
		t.Errorf("splitter recursive failed: %v", err)
	}
}

func TestSplitterMarkdown(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local splitter, err = text.splitter.markdown({chunk_size = 200})
		if err then
			error("unexpected error: " .. tostring(err))
		end
		if splitter == nil then
			error("splitter should not be nil")
		end
	`)
	if err != nil {
		t.Errorf("splitter markdown failed: %v", err)
	}
}

func TestSplitterSplitText(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local splitter, _ = text.splitter.recursive({chunk_size = 50, chunk_overlap = 10})
		local longText = "This is a long text that needs to be split into multiple chunks. Each chunk should be around 50 characters long with some overlap between them."
		local chunks, err = splitter:split_text(longText)
		if err then
			error("unexpected error: " .. tostring(err))
		end
		if #chunks == 0 then
			error("expected at least one chunk")
		end
	`)
	if err != nil {
		t.Errorf("splitter split_text failed: %v", err)
	}
}

func TestSplitterSplitBatch(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local splitter, _ = text.splitter.recursive({chunk_size = 50})
		local pages = {
			{content = "First page content that is long enough to be split", metadata = {page = 1}},
			{content = "Second page content that is also fairly long", metadata = {page = 2}}
		}
		local chunks, err = splitter:split_batch(pages)
		if err then
			error("unexpected error: " .. tostring(err))
		end
		if #chunks == 0 then
			error("expected at least one chunk")
		end
		-- Check metadata is preserved
		if chunks[1].metadata == nil then
			error("metadata should be preserved")
		end
	`)
	if err != nil {
		t.Errorf("splitter split_batch failed: %v", err)
	}
}
