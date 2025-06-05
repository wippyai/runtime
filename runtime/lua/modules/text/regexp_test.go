package text

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestRegexpModule(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	// Load the text module
	module := NewTextModule()
	l.PreloadModule("text", module.Loader)

	t.Run("DocumentPageExtraction", func(t *testing.T) {
		script := `
			local text = require("text")
			
			-- Test document content with pages
			local content = [[
<document-page number="1">First page content</document-page>
<document-page number="2">Second page content</document-page>
<document-page number="3">Third page content</document-page>
			]]
			
			-- Compile regex for page extraction (your exact use case)
			local re, err = text.regexp.compile('<document-page number="(\\d+)">(.*?)</document-page>')
			assert(re ~= nil, "Failed to compile regex: " .. (err or "unknown error"))
			assert(err == nil, "Unexpected error: " .. (err or ""))
			
			-- Find all pages with captures
			local matches = re:find_all_string_submatch(content)
			assert(#matches == 3, "Expected 3 matches, got " .. #matches)
			
			-- Check matches: [1] = full match, [2] = page number, [3] = content
			assert(matches[1][2] == "1", "Expected page '1', got '" .. (matches[1][2] or "nil") .. "'")
			assert(matches[1][3] == "First page content", "Wrong content for page 1")
			
			assert(matches[2][2] == "2", "Expected page '2', got '" .. (matches[2][2] or "nil") .. "'")
			assert(matches[2][3] == "Second page content", "Wrong content for page 2")
			
			assert(matches[3][2] == "3", "Expected page '3', got '" .. (matches[3][2] or "nil") .. "'")
			assert(matches[3][3] == "Third page content", "Wrong content for page 3")
			
			return true
		`

		err := l.DoString(script)
		if err != nil {
			t.Fatalf("Script failed: %v", err)
		}
	})

	t.Run("TagStripping", func(t *testing.T) {
		script := `
			local text = require("text")
			
			local html_content = "<h1>Title</h1><p>Some <b>bold</b> text.</p><div>More content</div>"
			
			-- Compile regex for tag removal
			local tag_re, err = text.regexp.compile('<[^>]*>')
			assert(tag_re ~= nil, "Failed to compile tag regex")
			assert(err == nil, "Tag regex error: " .. (err or ""))
			
			-- Strip all tags
			local clean_text = tag_re:replace_all_string(html_content, '')
			assert(clean_text == "TitleSome bold text.More content", "Tag stripping failed: " .. clean_text)
			
			return true
		`

		err := l.DoString(script)
		if err != nil {
			t.Fatalf("Tag stripping test failed: %v", err)
		}
	})

	t.Run("MatchingAndSplitting", func(t *testing.T) {
		script := `
			local text = require("text")
			
			local content = "apple,banana,cherry,date"
			
			-- Test matching
			local comma_re, err = text.regexp.compile(',')
			assert(comma_re ~= nil, "Failed to compile comma regex")
			assert(err == nil, "Comma regex error")
			
			local has_commas = comma_re:match_string(content)
			assert(has_commas == true, "Should match commas")
			
			-- Test splitting
			local parts = comma_re:split(content, -1)
			assert(#parts == 4, "Expected 4 parts, got " .. #parts)
			assert(parts[1] == "apple", "Expected 'apple', got '" .. parts[1] .. "'")
			assert(parts[2] == "banana", "Expected 'banana', got '" .. parts[2] .. "'")
			assert(parts[3] == "cherry", "Expected 'cherry', got '" .. parts[3] .. "'")
			assert(parts[4] == "date", "Expected 'date', got '" .. parts[4] .. "'")
			
			-- Test limited splitting
			local limited_parts = comma_re:split(content, 2)
			assert(#limited_parts == 2, "Expected 2 parts with limit, got " .. #limited_parts)
			assert(limited_parts[1] == "apple", "Expected 'apple'")
			assert(limited_parts[2] == "banana,cherry,date", "Expected remaining content")
			
			return true
		`

		err := l.DoString(script)
		if err != nil {
			t.Fatalf("Matching and splitting test failed: %v", err)
		}
	})

	t.Run("SingleMatches", func(t *testing.T) {
		script := `
			local text = require("text")
			
			local content = "Email: user@example.com and another: admin@test.org"
			
			-- Test single match with capture
			local email_re, err = text.regexp.compile('([a-zA-Z0-9._%+-]+)@([a-zA-Z0-9.-]+\\.[a-zA-Z]{2,})')
			assert(email_re ~= nil, "Failed to compile email regex")
			assert(err == nil, "Email regex error")
			
			-- Find first email
			local first_match = email_re:find_string_submatch(content)
			assert(first_match ~= nil, "Should find first email")
			assert(first_match[1] == "user@example.com", "Wrong full match: " .. (first_match[1] or "nil"))
			assert(first_match[2] == "user", "Wrong username: " .. (first_match[2] or "nil"))
			assert(first_match[3] == "example.com", "Wrong domain: " .. (first_match[3] or "nil"))
			
			-- Find all emails without captures
			local all_emails = email_re:find_all_string(content)
			assert(#all_emails == 2, "Expected 2 emails, got " .. #all_emails)
			assert(all_emails[1] == "user@example.com", "Wrong first email")
			assert(all_emails[2] == "admin@test.org", "Wrong second email")
			
			return true
		`

		err := l.DoString(script)
		if err != nil {
			t.Fatalf("Single matches test failed: %v", err)
		}
	})

	t.Run("ErrorHandling", func(t *testing.T) {
		script := `
			local text = require("text")
			
			-- Test invalid regex
			local re, err = text.regexp.compile('[invalid')
			assert(re == nil, "Should fail to compile invalid regex")
			assert(err ~= nil, "Should return error for invalid regex")
			assert(string.find(err, "error"), "Error should mention 'error'")
			
			return true
		`

		err := l.DoString(script)
		if err != nil {
			t.Fatalf("Error handling test failed: %v", err)
		}
	})

	t.Run("IndexMethods", func(t *testing.T) {
		script := `
			local text = require("text")
			
			local content = "The page1 and page2 are here"
			-- Characters:  T h e   p a g e 1   a n d   p a g e 2   a r e   h e r e
			-- Go 0-based: 0 1 2 3 4 5 6 7 8 9 ...
			-- Lua 1-based:1 2 3 4 5 6 7 8 9 10...
			-- "page1" is at Go [4,9) -> Lua [5,9]
			
			local re, err = text.regexp.compile('page\\d+')
			assert(re ~= nil, "Failed to compile regex")
			assert(err == nil, "Compile error")
			
			-- Test find_all_string_index 
			local all_positions = re:find_all_string_index(content)
			assert(#all_positions == 2, "Expected 2 matches, got " .. #all_positions)
			
			-- First match "page1": Go [4,9) -> Lua [5,9]
			assert(all_positions[1][1] == 5, "Expected start 5, got " .. all_positions[1][1])
			assert(all_positions[1][2] == 9, "Expected end 9, got " .. all_positions[1][2])
			
			-- Verify we can extract the match using Lua string.sub
			local extracted = content:sub(all_positions[1][1], all_positions[1][2])
			assert(extracted == "page1", "Expected 'page1', got '" .. extracted .. "'")
			
			-- Test find_string_index (first match only)
			local first_pos = re:find_string_index(content) 
			assert(first_pos[1] == 5, "Expected first start 5")
			assert(first_pos[2] == 9, "Expected first end 9")
			
			-- Test no matches
			local no_re, _ = text.regexp.compile('xyz')
			assert(no_re:find_all_string_index(content) == nil, "Expected nil for no matches")
			assert(no_re:find_string_index(content) == nil, "Expected nil for no match")
			
			return true
		`

		err := l.DoString(script)
		if err != nil {
			t.Fatalf("Index methods test failed: %v", err)
		}
	})

	t.Run("IntrospectionMethods", func(t *testing.T) {
		script := `
			local text = require("text")
			
			-- Test unnamed captures
			local re1, err = text.regexp.compile('page(\\d+)')
			assert(re1 ~= nil and err == nil, "Failed to compile regex")
			
			local num_groups = re1:num_subexp()
			assert(num_groups == 1, "Expected 1 capture group, got " .. num_groups)
			
			local names = re1:subexp_names() 
			-- Go returns ["", ""] for 1 unnamed capture (full match + 1 capture)
			assert(#names == 2, "Expected 2 names, got " .. #names)
			assert(names[1] == "", "Expected empty string for full match")
			assert(names[2] == "", "Expected empty string for unnamed capture")
			
			-- Test named captures
			local named_re, _ = text.regexp.compile('(?P<num>\\d+)-(?P<word>[a-z]+)')
			assert(named_re ~= nil, "Failed to compile named regex")
			
			assert(named_re:num_subexp() == 2, "Expected 2 named groups")
			
			local named_names = named_re:subexp_names()
			-- Go returns ["", "num", "word"] for 2 named captures
			assert(#named_names == 3, "Expected 3 names for named regex")
			assert(named_names[1] == "", "Expected empty for full match")
			assert(named_names[2] == "num", "Expected 'num' for first capture")
			assert(named_names[3] == "word", "Expected 'word' for second capture")
			
			return true
		`

		err := l.DoString(script)
		if err != nil {
			t.Fatalf("Introspection methods test failed: %v", err)
		}
	})
}
