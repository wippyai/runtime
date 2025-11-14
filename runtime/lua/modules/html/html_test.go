package html

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestHTMLModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewHTMLModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local html = require("html")
			assert(type(html) == "table", "module should be a table")
			assert(type(html.sanitize) == "table", "sanitize should be a table")
			assert(type(html.sanitize.new_policy) == "function", "new_policy should be a function")
			assert(type(html.sanitize.ugc_policy) == "function", "ugc_policy should be a function")
			assert(type(html.sanitize.strict_policy) == "function", "strict_policy should be a function")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("policy creation", func(t *testing.T) {
		testCases := []struct {
			name   string
			script string
		}{
			{
				name: "new_policy creation",
				script: `
					local html = require("html")
					local policy, err = html.sanitize.new_policy()
					assert(policy ~= nil, "policy should not be nil")
					assert(err == nil, "error should be nil")
					assert(type(policy.sanitize) == "function", "should have sanitize method")
					assert(type(policy.allow_elements) == "function", "should have allow_elements method")
					assert(type(policy.allow_attrs) == "function", "should have allow_attrs method")
				`,
			},
			{
				name: "ugc_policy creation",
				script: `
					local html = require("html")
					local policy, err = html.sanitize.ugc_policy()
					assert(policy ~= nil, "policy should not be nil")
					assert(err == nil, "error should be nil")
					assert(type(policy.sanitize) == "function", "should have sanitize method")
				`,
			},
			{
				name: "strict_policy creation",
				script: `
					local html = require("html")
					local policy, err = html.sanitize.strict_policy()
					assert(policy ~= nil, "policy should not be nil")
					assert(err == nil, "error should be nil")
					assert(type(policy.sanitize) == "function", "should have sanitize method")
				`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewHTMLModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				assert.NoError(t, err)
			})
		}
	})

	t.Run("basic sanitization", func(t *testing.T) {
		testCases := []struct {
			name           string
			script         string
			expectedResult string
		}{
			{
				name: "strict policy strips everything",
				script: `
					local html = require("html")
					local policy, err = html.sanitize.strict_policy()
					assert(err == nil, "error creating policy should be nil")
					
					local result = policy:sanitize('<p>Hello <script>alert("xss")</script> world</p>')
					return result
				`,
				expectedResult: "Hello  world",
			},
			{
				name: "ugc policy allows basic formatting",
				script: `
					local html = require("html")
					local policy, err = html.sanitize.ugc_policy()
					assert(err == nil, "error creating policy should be nil")
					
					local result = policy:sanitize('<p>Hello <strong>world</strong></p>')
					return result
				`,
				expectedResult: "<p>Hello <strong>world</strong></p>",
			},
			{
				name: "ugc policy removes script tags",
				script: `
					local html = require("html")
					local policy, err = html.sanitize.ugc_policy()
					assert(err == nil, "error creating policy should be nil")
					
					local result = policy:sanitize('<p>Hello <script>alert("xss")</script> world</p>')
					return result
				`,
				expectedResult: "<p>Hello  world</p>",
			},
			{
				name: "new policy allows nothing by default",
				script: `
					local html = require("html")
					local policy, err = html.sanitize.new_policy()
					assert(err == nil, "error creating policy should be nil")
					
					local result = policy:sanitize('<p>Hello <strong>world</strong></p>')
					return result
				`,
				expectedResult: "Hello world",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewHTMLModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				require.NoError(t, err)

				result := vm.State().Get(-1)
				assert.Equal(t, tc.expectedResult, result.String())
				vm.State().Pop(1)
			})
		}
	})

	t.Run("policy building", func(t *testing.T) {
		mod := NewHTMLModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local html = require("html")
			local policy, err = html.sanitize.new_policy()
			assert(err == nil, "error creating policy should be nil")
			
			-- Build custom policy
			policy:allow_elements("p", "strong", "em")
			policy:allow_attrs("class"):on_elements("p")
			
			local test_cases = {
				{
					input = '<p class="intro">Hello <strong>world</strong></p>',
					expected = '<p class="intro">Hello <strong>world</strong></p>'
				},
				{
					input = '<p>Hello <script>alert("xss")</script> world</p>',
					expected = '<p>Hello  world</p>'
				},
				{
					input = '<div>Not allowed</div>',
					expected = 'Not allowed'
				},
				{
					input = '<p class="intro"><em>Emphasis</em> text</p>',
					expected = '<p class="intro"><em>Emphasis</em> text</p>'
				}
			}
			
			local results = {}
			for i, case in ipairs(test_cases) do
				local result = policy:sanitize(case.input)
				results[i] = {
					input = case.input,
					expected = case.expected,
					actual = result,
					matches = (result == case.expected)
				}
			end
			
			return results
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)

		result := vm.State().Get(-1).(*lua.LTable)
		var allMatch = true

		result.ForEach(func(_, value lua.LValue) {
			caseTable := value.(*lua.LTable)
			matches := bool(caseTable.RawGetString("matches").(lua.LBool))
			if !matches {
				input := caseTable.RawGetString("input").String()
				expected := caseTable.RawGetString("expected").String()
				actual := caseTable.RawGetString("actual").String()
				t.Errorf("Case %s failed: expected %q, got %q", input, expected, actual)
				allMatch = false
			}
		})

		assert.True(t, allMatch)
	})

	t.Run("attr builder functionality", func(t *testing.T) {
		mod := NewHTMLModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local html = require("html")
			local policy, err = html.sanitize.new_policy()
			assert(err == nil, "error creating policy should be nil")
			
			-- Test attr builder patterns
			policy:allow_elements("p", "span", "a")
			policy:allow_attrs("class"):globally()
			policy:allow_attrs("href"):on_elements("a")
			policy:allow_attrs("style"):matching("^color:#[0-9a-fA-F]{6}$"):on_elements("span")
			
			local test_cases = {
				{
					input = '<p class="text">Paragraph</p>',
					expected = '<p class="text">Paragraph</p>',
					desc = "global class attribute"
				},
				{
					input = '<a href="http://example.com" class="link">Link</a>',
					expected = '<a href="http://example.com" class="link">Link</a>',
					desc = "href on anchor with global class"
				},
				{
					input = '<span style="color:#ff0000">Red text</span>',
					expected = '<span style="color:#ff0000">Red text</span>',
					desc = "valid color style"
				},
				{
					input = '<span style="background:red">Invalid style</span>',
					expected = '<span>Invalid style</span>',
					desc = "invalid style pattern"
				},
				{
					input = '<p href="bad">Wrong element for href</p>',
					expected = '<p>Wrong element for href</p>',
					desc = "href not allowed on p"
				}
			}
			
			local results = {}
			for i, case in ipairs(test_cases) do
				local result = policy:sanitize(case.input)
				results[i] = {
					desc = case.desc,
					expected = case.expected,
					actual = result,
					matches = (result == case.expected)
				}
			end
			
			return results
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)

		result := vm.State().Get(-1).(*lua.LTable)
		var allMatch = true

		result.ForEach(func(_, value lua.LValue) {
			caseTable := value.(*lua.LTable)
			matches := bool(caseTable.RawGetString("matches").(lua.LBool))
			if !matches {
				desc := caseTable.RawGetString("desc").String()
				expected := caseTable.RawGetString("expected").String()
				actual := caseTable.RawGetString("actual").String()
				t.Errorf("Case '%s' failed: expected %q, got %q", desc, expected, actual)
				allMatch = false
			}
		})

		assert.True(t, allMatch)
	})

	t.Run("url security features", func(t *testing.T) {
		mod := NewHTMLModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local html = require("html")
			local policy, err = html.sanitize.new_policy()
			assert(err == nil, "error creating policy should be nil")
			
			policy:allow_elements("a")
			policy:allow_attrs("href"):on_elements("a")
			policy:allow_url_schemes("https", "mailto")
			policy:require_nofollow_on_links(true)
			
			local test_cases = {
				{
					input = '<a href="https://example.com">Safe HTTPS</a>',
					should_contain = 'href="https://example.com"',
					should_contain2 = 'rel="nofollow"'
				},
				{
					input = '<a href="mailto:user@example.com">Email</a>',
					should_contain = 'href="mailto:user@example.com"'
				},
				{
					input = '<a href="http://example.com">HTTP not allowed</a>',
					expected = 'HTTP not allowed'
				},
				{
					input = '<a href="javascript:alert()">JS not allowed</a>',
					expected = 'JS not allowed'
				}
			}
			
			local results = {}
			for i, case in ipairs(test_cases) do
				local result = policy:sanitize(case.input)
				results[i] = {
					input = case.input,
					result = result,
					expected = case.expected
				}
				
				if case.should_contain then
					results[i].contains_check = string.find(result, case.should_contain, 1, true) ~= nil
				end
				if case.should_contain2 then
					results[i].contains_check2 = string.find(result, case.should_contain2, 1, true) ~= nil
				end
			end
			
			return results
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)

		result := vm.State().Get(-1).(*lua.LTable)

		result.ForEach(func(_, value lua.LValue) {
			caseTable := value.(*lua.LTable)
			input := caseTable.RawGetString("input").String()
			resultStr := caseTable.RawGetString("result").String()

			if expected := caseTable.RawGetString("expected"); expected.Type() != lua.LTNil {
				assert.Equal(t, expected.String(), resultStr, "Input: %s", input)
			}

			if containsCheck := caseTable.RawGetString("contains_check"); containsCheck.Type() != lua.LTNil {
				assert.True(t, bool(containsCheck.(lua.LBool)), "Should contain expected content. Input: %s, Result: %s", input, resultStr)
			}

			if containsCheck2 := caseTable.RawGetString("contains_check2"); containsCheck2.Type() != lua.LTNil {
				assert.True(t, bool(containsCheck2.(lua.LBool)), "Should contain second expected content. Input: %s, Result: %s", input, resultStr)
			}
		})
	})

	t.Run("convenience methods", func(t *testing.T) {
		mod := NewHTMLModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local html = require("html")
			local policy, err = html.sanitize.new_policy()
			assert(err == nil, "error creating policy should be nil")
			
			policy:allow_standard_attributes()
			policy:allow_images()
			policy:allow_lists()
			policy:allow_tables()
			
			local test_inputs = {
				{
					input = '<img src="test.jpg" alt="test" id="photo" title="A photo">',
					should_preserve = true,
					desc = "image with standard attributes"
				},
				{
					input = '<ul id="list"><li title="item">Item 1</li><li>Item 2</li></ul>',
					should_preserve = true,
					desc = "list with standard attributes"
				},
				{
					input = '<table id="data"><tr><td title="cell">Cell</td></tr></table>',
					should_preserve = true,  
					desc = "table with standard attributes"
				},
				{
					input = '<span id="span" lang="en" dir="ltr" title="span">Text</span>',
					should_preserve = false,
					desc = "span element should be stripped"
				}
			}
			
			local results = {}
			for i, test in ipairs(test_inputs) do
				local result = policy:sanitize(test.input)
				local has_substantial_content = #result > 10
				results[i] = {
					input = test.input,
					result = result,
					desc = test.desc,
					should_preserve = test.should_preserve,
					has_content = has_substantial_content,
					test_passes = (test.should_preserve == has_substantial_content)
				}
			end
			
			return results
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)

		result := vm.State().Get(-1).(*lua.LTable)

		result.ForEach(func(_, value lua.LValue) {
			caseTable := value.(*lua.LTable)
			testPasses := bool(caseTable.RawGetString("test_passes").(lua.LBool))
			desc := caseTable.RawGetString("desc").String()
			input := caseTable.RawGetString("input").String()
			result := caseTable.RawGetString("result").String()
			assert.True(t, testPasses, "Test case '%s' failed. Input: %s, Result: %s", desc, input, result)
		})
	})

	t.Run("data uri images", func(t *testing.T) {
		mod := NewHTMLModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local html = require("html")
			local policy, err = html.sanitize.new_policy()
			assert(err == nil, "error creating policy should be nil")
			
			policy:allow_elements("img")
			policy:allow_attrs("src"):on_elements("img")
			policy:allow_data_uri_images()
			
			local test_cases = {
				{
					input = '<img src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==">',
					should_preserve = true,
					desc = "valid PNG data URI"
				},
				{
					input = '<img src="data:image/jpeg;base64,/9j/4AAQSkZJRgABAQEAYABgAAD/2wBDAAYEBQYFBAYGBQYHBwYIChAKCgkJChQODwwQFxQYGBcUFhYaHSUfGhsjHBYWICwgIyYnKSopGR8tMC0oMCUoKSj/2wBDAQcHBwoIChMKChMoGhYaKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCj/wAARCAABAAEDASIAAhEBAxEB/8QAFQABAQAAAAAAAAAAAAAAAAAAAAv/xAAUEAEAAAAAAAAAAAAAAAAAAAAA/8QAFQEBAQAAAAAAAAAAAAAAAAAAAAX/xAAUEQEAAAAAAAAAAAAAAAAAAAAA/9oADAMBAAIRAxEAPwCdABmX/9k=">',
					should_preserve = true,
					desc = "valid JPEG data URI"
				},
				{
					input = '<img src="data:text/html;base64,PHNjcmlwdD5hbGVydCgneHNzJyk8L3NjcmlwdD4=">',
					should_preserve = false,
					desc = "HTML data URI should be blocked"
				},
				{
					input = '<img src="http://example.com/image.jpg">',
					should_preserve = false,
					desc = "regular HTTP URLs should be blocked without allow_url_schemes"
				}
			}
			
			local results = {}
			for i, case in ipairs(test_cases) do
				local result = policy:sanitize(case.input)
				local has_src = string.find(result, "src=", 1, true) ~= nil
				results[i] = {
					desc = case.desc,
					result = result,
					should_preserve = case.should_preserve,
					has_src = has_src,
					test_passes = (case.should_preserve == has_src)
				}
			end
			
			return results
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)

		result := vm.State().Get(-1).(*lua.LTable)

		result.ForEach(func(_, value lua.LValue) {
			caseTable := value.(*lua.LTable)
			testPasses := bool(caseTable.RawGetString("test_passes").(lua.LBool))
			desc := caseTable.RawGetString("desc").String()
			resultStr := caseTable.RawGetString("result").String()
			assert.True(t, testPasses, "Data URI test '%s' failed. Result: %s", desc, resultStr)
		})
	})

	t.Run("regex validation errors", func(t *testing.T) {
		mod := NewHTMLModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local html = require("html")
			local policy, err = html.sanitize.new_policy()
			assert(err == nil, "error creating policy should be nil")
			
			policy:allow_elements("span")
			
			-- Test invalid regex pattern
			local success, err = pcall(function()
				policy:allow_attrs("class"):matching("[invalid regex"):on_elements("span")
			end)
			
			-- Should not crash, should handle error gracefully
			assert(not success, "invalid regex should cause error")
			assert(type(err) == "string", "should return error message")
			
			return true
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.NoError(t, err)
	})

	t.Run("malformed html handling", func(t *testing.T) {
		mod := NewHTMLModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local html = require("html")
			local policy, err = html.sanitize.ugc_policy()
			assert(err == nil, "error creating policy should be nil")
			
			local malformed_inputs = {
				'<p>Unclosed paragraph',
				'<div><p>Nested unclosed</div>',
				'<img src="test.jpg" alt="missing quote>',
				'<<invalid>>double brackets<</invalid>>',
				'<p class=>Empty attribute value</p>',
				'&lt;escaped&gt; content &amp; entities'
			}
			
			local results = {}
			for i, input in ipairs(malformed_inputs) do
				local result = policy:sanitize(input)
				-- Should not crash and should return a string
				results[i] = {
					input = input,
					result = result,
					result_type = type(result),
					not_empty = #result > 0
				}
			end
			
			return results
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)

		result := vm.State().Get(-1).(*lua.LTable)

		result.ForEach(func(_, value lua.LValue) {
			caseTable := value.(*lua.LTable)
			resultType := caseTable.RawGetString("result_type").String()
			input := caseTable.RawGetString("input").String()
			assert.Equal(t, "string", resultType, "Should always return string for input: %s", input)
		})
	})

	t.Run("method chaining and multiple calls", func(t *testing.T) {
		mod := NewHTMLModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local html = require("html")
			local policy, err = html.sanitize.new_policy()
			assert(err == nil, "error creating policy should be nil")
			
			-- Test multiple calls to same method
			policy:allow_elements("p", "strong")
			policy:allow_elements("em", "span")  -- Should add to existing
			
			-- Test chaining: allow_attrs returns AttrBuilder, globally() returns policy
			local attr_builder = policy:allow_attrs("class")
			local chained_policy = attr_builder:globally()
			assert(chained_policy == policy, "globally() should return original policy object")
			
			local result = policy:sanitize('<p class="test">Para</p><strong>Bold</strong><em>Italic</em><span>Span</span>')
			
			-- All elements should be preserved
			local has_p = string.find(result, "<p", 1, true) ~= nil
			local has_strong = string.find(result, "<strong", 1, true) ~= nil  
			local has_em = string.find(result, "<em", 1, true) ~= nil
			local has_span = string.find(result, "<span", 1, true) ~= nil
			local has_class = string.find(result, 'class="test"', 1, true) ~= nil
			
			return {
				result = result,
				has_p = has_p,
				has_strong = has_strong,
				has_em = has_em,
				has_span = has_span,
				has_class = has_class,
				all_preserved = has_p and has_strong and has_em and has_span and has_class
			}
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)

		result := vm.State().Get(-1).(*lua.LTable)
		allPreserved := bool(result.RawGetString("all_preserved").(lua.LBool))
		resultStr := result.RawGetString("result").String()
		assert.True(t, allPreserved, "Multiple allow_elements calls should accumulate. Result: %s", resultStr)
	})

	t.Run("edge cases and error handling", func(t *testing.T) {
		testCases := []struct {
			name   string
			script string
		}{
			{
				name: "empty string",
				script: `
					local html = require("html")
					local policy, err = html.sanitize.ugc_policy()
					assert(err == nil, "error creating policy should be nil")
					
					local result = policy:sanitize("")
					assert(result == "", "empty string should remain empty")
				`,
			},
			{
				name: "plain text",
				script: `
					local html = require("html")
					local policy, err = html.sanitize.ugc_policy()
					assert(err == nil, "error creating policy should be nil")
					
					local result = policy:sanitize("Just plain text")
					assert(result == "Just plain text", "plain text should pass through")
				`,
			},
			{
				name: "unicode and special characters",
				script: `
					local html = require("html")
					local policy, err = html.sanitize.ugc_policy()
					assert(err == nil, "error creating policy should be nil")
					
					local result = policy:sanitize('<p>Hello 👋 世界 &amp; café</p>')
					assert(string.find(result, "👋"), "should preserve unicode emoji")
					assert(string.find(result, "世界"), "should preserve unicode text")
					assert(string.find(result, "&amp;"), "should preserve entities")
				`,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewHTMLModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				assert.NoError(t, err)
			})
		}
	})
}
