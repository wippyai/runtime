package html

import (
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestBind(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	Module.Load(l)

	mod := l.GetGlobal("html")
	if mod.Type() != lua.LTTable {
		t.Fatal("html module not registered")
	}

	tbl := mod.(*lua.LTable)
	sanitize := tbl.RawGetString("sanitize")
	if sanitize.Type() != lua.LTTable {
		t.Fatal("html.sanitize not registered")
	}

	sanitizeTbl := sanitize.(*lua.LTable)
	funcs := []string{"new_policy", "ugc_policy", "strict_policy"}
	for _, fn := range funcs {
		if sanitizeTbl.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("%s function not registered", fn)
		}
	}
}

func TestNewPolicy(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy, err = html.sanitize.new_policy()
		if policy == nil then
			error("policy should not be nil")
		end
		if err ~= nil then
			error("err should be nil")
		end
	`)
	if err != nil {
		t.Errorf("new_policy test failed: %v", err)
	}
}

func TestUGCPolicy(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy, err = html.sanitize.ugc_policy()
		if policy == nil then
			error("policy should not be nil")
		end
		if err ~= nil then
			error("err should be nil")
		end
	`)
	if err != nil {
		t.Errorf("ugc_policy test failed: %v", err)
	}
}

func TestStrictPolicy(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy, err = html.sanitize.strict_policy()
		if policy == nil then
			error("policy should not be nil")
		end
		if err ~= nil then
			error("err should be nil")
		end
	`)
	if err != nil {
		t.Errorf("strict_policy test failed: %v", err)
	}
}

func TestStrictPolicySanitize(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.strict_policy()
		local result = policy:sanitize('<p>Hello <script>alert("xss")</script> world</p>')
		if result ~= "Hello  world" then
			error("expected 'Hello  world', got: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("strict sanitize test failed: %v", err)
	}
}

func TestUGCPolicySanitize(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.ugc_policy()
		local result = policy:sanitize('<p>Hello <strong>world</strong></p>')
		if result ~= '<p>Hello <strong>world</strong></p>' then
			error("unexpected result: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("ugc sanitize test failed: %v", err)
	}
}

func TestUGCRemovesScript(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.ugc_policy()
		local result = policy:sanitize('<p>Hello <script>alert("xss")</script> world</p>')
		if result ~= '<p>Hello  world</p>' then
			error("unexpected result: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("ugc script removal test failed: %v", err)
	}
}

func TestNewPolicyAllowsNothing(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		local result = policy:sanitize('<p>Hello <strong>world</strong></p>')
		if result ~= "Hello world" then
			error("expected 'Hello world', got: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("new_policy allows nothing test failed: %v", err)
	}
}

func TestAllowElements(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("p", "strong")
		local result = policy:sanitize('<p>Hello <strong>world</strong></p>')
		if result ~= '<p>Hello <strong>world</strong></p>' then
			error("unexpected result: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("allow_elements test failed: %v", err)
	}
}

func TestAllowAttrsOnElements(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("p")
		policy:allow_attrs("class"):on_elements("p")
		local result = policy:sanitize('<p class="intro">Hello</p>')
		if result ~= '<p class="intro">Hello</p>' then
			error("unexpected result: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("allow_attrs on_elements test failed: %v", err)
	}
}

func TestAllowAttrsGlobally(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("p", "span")
		policy:allow_attrs("class"):globally()
		local result = policy:sanitize('<p class="text"><span class="inner">Hello</span></p>')
		if not string.find(result, 'class="text"') then
			error("class on p not preserved: " .. result)
		end
		if not string.find(result, 'class="inner"') then
			error("class on span not preserved: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("allow_attrs globally test failed: %v", err)
	}
}

func TestAllowAttrsMatching(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("span")
		local builder, err = policy:allow_attrs("style"):matching("^color:#[0-9a-fA-F]{6}$")
		if err ~= nil then
			error("matching should not return error: " .. err)
		end
		builder:on_elements("span")

		local valid = policy:sanitize('<span style="color:#ff0000">Red</span>')
		if not string.find(valid, 'style="color:#ff0000"') then
			error("valid style should be preserved: " .. valid)
		end

		local invalid = policy:sanitize('<span style="background:red">Invalid</span>')
		if string.find(invalid, 'style=') then
			error("invalid style should be removed: " .. invalid)
		end
	`)
	if err != nil {
		t.Errorf("allow_attrs matching test failed: %v", err)
	}
}

func TestMatchingInvalidRegex(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("span")
		local builder, err = policy:allow_attrs("class"):matching("[invalid")
		if builder ~= nil then
			error("builder should be nil for invalid regex")
		end
		if err == nil then
			error("error should not be nil for invalid regex")
		end
	`)
	if err != nil {
		t.Errorf("matching invalid regex test failed: %v", err)
	}
}

func TestAllowStandardURLs(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("a")
		policy:allow_attrs("href"):on_elements("a")
		policy:allow_standard_urls()
		local result = policy:sanitize('<a href="https://example.com">Link</a>')
		if not string.find(result, 'href="https://example.com"') then
			error("href not preserved: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("allow_standard_urls test failed: %v", err)
	}
}

func TestRequireParseableURLs(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("a")
		policy:allow_attrs("href"):on_elements("a")
		policy:require_parseable_urls(true)
		local result = policy:sanitize('<a href="https://example.com">Link</a>')
		if type(result) ~= "string" then
			error("should return string")
		end
	`)
	if err != nil {
		t.Errorf("require_parseable_urls test failed: %v", err)
	}
}

func TestAllowRelativeURLs(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("a")
		policy:allow_attrs("href"):on_elements("a")
		policy:allow_relative_urls(true)
		local result = policy:sanitize('<a href="/page">Link</a>')
		if not string.find(result, 'href="/page"') then
			error("relative URL not preserved: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("allow_relative_urls test failed: %v", err)
	}
}

func TestAllowURLSchemes(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("a")
		policy:allow_attrs("href"):on_elements("a")
		policy:allow_url_schemes("https", "mailto")

		local https_result = policy:sanitize('<a href="https://example.com">Link</a>')
		if not string.find(https_result, 'href="https://example.com"') then
			error("https URL not preserved: " .. https_result)
		end

		local mailto_result = policy:sanitize('<a href="mailto:test@example.com">Email</a>')
		if not string.find(mailto_result, 'href="mailto:test@example.com"') then
			error("mailto URL not preserved: " .. mailto_result)
		end
	`)
	if err != nil {
		t.Errorf("allow_url_schemes test failed: %v", err)
	}
}

func TestRequireNoFollowOnLinks(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("a")
		policy:allow_attrs("href", "rel"):on_elements("a")
		policy:allow_standard_urls()
		policy:require_nofollow_on_links(true)
		local result = policy:sanitize('<a href="https://example.com">Link</a>')
		if not string.find(result, 'rel="nofollow"') then
			error("nofollow not added: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("require_nofollow_on_links test failed: %v", err)
	}
}

func TestRequireNoReferrerOnLinks(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("a")
		policy:allow_attrs("href", "rel"):on_elements("a")
		policy:allow_standard_urls()
		policy:require_noreferrer_on_links(true)
		local result = policy:sanitize('<a href="https://example.com">Link</a>')
		if not string.find(result, 'noreferrer') then
			error("noreferrer not added: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("require_noreferrer_on_links test failed: %v", err)
	}
}

func TestAddTargetBlankToFullyQualifiedLinks(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("a")
		policy:allow_attrs("href", "target"):on_elements("a")
		policy:allow_standard_urls()
		policy:add_target_blank_to_fully_qualified_links(true)
		local result = policy:sanitize('<a href="https://example.com">Link</a>')
		if not string.find(result, 'target="_blank"') then
			error("target _blank not added: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("add_target_blank test failed: %v", err)
	}
}

func TestAllowDataURIImages(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("img")
		policy:allow_attrs("src"):on_elements("img")
		policy:allow_data_uri_images()
		local result = policy:sanitize('<img src="data:image/png;base64,iVBORw0KGgo=">')
		if not string.find(result, 'src="data:image/png') then
			error("data URI not preserved: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("allow_data_uri_images test failed: %v", err)
	}
}

func TestAllowStandardAttributes(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("p")
		policy:allow_standard_attributes()
		local result = policy:sanitize('<p id="para" class="text" title="Para">Hello</p>')
		if not string.find(result, 'id="para"') then
			error("id not preserved: " .. result)
		end
		if not string.find(result, 'title="Para"') then
			error("title not preserved: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("allow_standard_attributes test failed: %v", err)
	}
}

func TestAllowImages(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_images()
		local result = policy:sanitize('<img src="test.jpg" alt="Test">')
		if not string.find(result, '<img') then
			error("img element not preserved: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("allow_images test failed: %v", err)
	}
}

func TestAllowLists(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_lists()
		local result = policy:sanitize('<ul><li>Item 1</li><li>Item 2</li></ul>')
		if not string.find(result, '<ul>') then
			error("ul not preserved: " .. result)
		end
		if not string.find(result, '<li>') then
			error("li not preserved: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("allow_lists test failed: %v", err)
	}
}

func TestAllowTables(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_tables()
		local result = policy:sanitize('<table><tr><td>Cell</td></tr></table>')
		if not string.find(result, '<table>') then
			error("table not preserved: " .. result)
		end
		if not string.find(result, '<td>') then
			error("td not preserved: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("allow_tables test failed: %v", err)
	}
}

func TestMethodChaining(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("p")
		local returned = policy:allow_attrs("class"):on_elements("p")
		if returned ~= policy then
			error("on_elements should return original policy")
		end

		local returned2 = policy:allow_attrs("id"):globally()
		if returned2 ~= policy then
			error("globally should return original policy")
		end
	`)
	if err != nil {
		t.Errorf("method chaining test failed: %v", err)
	}
}

func TestPolicyMethods(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	methods := []string{
		"allow_elements",
		"allow_attrs",
		"allow_standard_urls",
		"require_parseable_urls",
		"allow_relative_urls",
		"allow_url_schemes",
		"require_nofollow_on_links",
		"require_noreferrer_on_links",
		"add_target_blank_to_fully_qualified_links",
		"allow_data_uri_images",
		"allow_standard_attributes",
		"allow_images",
		"allow_lists",
		"allow_tables",
		"sanitize",
	}

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		return policy
	`)
	if err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	policy := l.Get(-1).(*lua.LUserData)
	mt := policy.Metatable.(*lua.LTable)
	index := mt.RawGetString("__index").(*lua.LTable)

	for _, method := range methods {
		if index.RawGetString(method).Type() != lua.LTFunction {
			t.Errorf("method %s not found", method)
		}
	}
}

func TestAttrBuilderMethods(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	methods := []string{"on_elements", "globally", "matching"}

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		return policy:allow_attrs("class")
	`)
	if err != nil {
		t.Fatalf("failed to create attr builder: %v", err)
	}

	builder := l.Get(-1).(*lua.LUserData)
	mt := builder.Metatable.(*lua.LTable)
	index := mt.RawGetString("__index").(*lua.LTable)

	for _, method := range methods {
		if index.RawGetString(method).Type() != lua.LTFunction {
			t.Errorf("method %s not found", method)
		}
	}
}

func TestEmptyString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.ugc_policy()
		local result = policy:sanitize("")
		if result ~= "" then
			error("empty string should remain empty")
		end
	`)
	if err != nil {
		t.Errorf("empty string test failed: %v", err)
	}
}

func TestPlainText(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.ugc_policy()
		local result = policy:sanitize("Just plain text")
		if result ~= "Just plain text" then
			error("plain text should pass through: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("plain text test failed: %v", err)
	}
}

func TestUnicode(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.ugc_policy()
		local result = policy:sanitize('<p>Hello world cafe</p>')
		if not string.find(result, "cafe") then
			error("unicode not preserved: " .. result)
		end
	`)
	if err != nil {
		t.Errorf("unicode test failed: %v", err)
	}
}

func TestMalformedHTML(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	testCases := []string{
		"<p>Unclosed paragraph",
		"<div><p>Nested unclosed</div>",
		"<<invalid>>double brackets<</invalid>>",
		"&lt;escaped&gt; content",
	}

	for _, tc := range testCases {
		err := l.DoString(`
			local policy = html.sanitize.ugc_policy()
			local result = policy:sanitize("` + tc + `")
			if type(result) ~= "string" then
				error("should return string")
			end
		`)
		if err != nil {
			t.Errorf("malformed HTML test failed for %q: %v", tc, err)
		}
	}
}

func TestInvalidPolicyArgument(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		local success, err = pcall(function()
			policy:sanitize(nil)
		end)
		if success then
			error("should have failed for nil input")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestInvalidAttrBuilderArgument(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		local success, err = pcall(function()
			policy:allow_attrs("class"):on_elements({})
		end)
		if success then
			error("should have failed for table element")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestXSSPrevention(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	xssPayloads := []struct {
		input    string
		notAllow string
	}{
		{`<script>alert('xss')</script>`, "script"},
		{`<img src=x onerror=alert('xss')>`, "onerror"},
		{`<a href="javascript:alert('xss')">`, "javascript"},
		{`<div onclick="alert('xss')">`, "onclick"},
		{`<svg onload="alert('xss')">`, "onload"},
		{`<body onload="alert('xss')">`, "onload"},
	}

	for _, tc := range xssPayloads {
		l2 := lua.NewState()
		Module.Load(l2)

		script := `
			local policy = html.sanitize.strict_policy()
			return policy:sanitize([[` + tc.input + `]])
		`
		err := l2.DoString(script)
		if err != nil {
			t.Errorf("XSS test failed for %q: %v", tc.input, err)
			l2.Close()
			continue
		}

		result := l2.Get(-1).String()
		if strings.Contains(strings.ToLower(result), tc.notAllow) {
			t.Errorf("XSS payload not sanitized: input=%q, result=%q, should not contain %q",
				tc.input, result, tc.notAllow)
		}
		l2.Close()
	}
}

func TestMultipleAllowElements(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("p", "strong")
		policy:allow_elements("em", "span")

		local result = policy:sanitize('<p><strong>Bold</strong><em>Italic</em><span>Span</span></p>')
		if not string.find(result, '<p>') then error("p not found") end
		if not string.find(result, '<strong>') then error("strong not found") end
		if not string.find(result, '<em>') then error("em not found") end
		if not string.find(result, '<span>') then error("span not found") end
	`)
	if err != nil {
		t.Errorf("multiple allow_elements test failed: %v", err)
	}
}

func TestMatchingWithGlobally(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local policy = html.sanitize.new_policy()
		policy:allow_elements("p", "span")
		local builder, err = policy:allow_attrs("class"):matching("^[a-z]+$")
		if err ~= nil then
			error("matching should not return error: " .. err)
		end
		builder:globally()

		local valid = policy:sanitize('<p class="intro"><span class="text">Hello</span></p>')
		if not string.find(valid, 'class="intro"') then
			error("valid class on p not preserved: " .. valid)
		end
		if not string.find(valid, 'class="text"') then
			error("valid class on span not preserved: " .. valid)
		end

		local invalid = policy:sanitize('<p class="invalid-class">Hello</p>')
		if string.find(invalid, 'class=') then
			error("invalid class should be removed: " .. invalid)
		end
	`)
	if err != nil {
		t.Errorf("matching with globally test failed: %v", err)
	}
}

func TestImmutability(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local mod = html
		local sanitize = mod.sanitize

		local success = pcall(function()
			mod.foo = "bar"
		end)

		local success2 = pcall(function()
			sanitize.foo = "bar"
		end)
	`)
	if err != nil {
		t.Errorf("immutability test failed: %v", err)
	}
}
