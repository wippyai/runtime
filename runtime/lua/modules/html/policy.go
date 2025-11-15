package html

import (
	"regexp"

	"github.com/microcosm-cc/bluemonday"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const (
	policyMetatable      = "bluemonday.Policy"
	attrBuilderMetatable = "bluemonday.AttrBuilder"
)

type PolicyWrapper struct {
	policy *bluemonday.Policy
}

type AttrBuilder struct {
	policy   *bluemonday.Policy
	policyUD *lua.LUserData // Store reference to original policy userdata
	attrs    []string
	regex    *regexp.Regexp
}

func checkPolicy(l *lua.LState) *PolicyWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*PolicyWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected Policy")
	return nil
}

func registerPolicy(l *lua.LState) {
	value.RegisterTypeMethods(l, policyMetatable, nil, map[string]lua.LGFunction{
		"allow_elements":                            policyAllowElements,
		"allow_attrs":                               policyAllowAttrs,
		"allow_standard_urls":                       policyAllowStandardURLs,
		"require_parseable_urls":                    policyRequireParseableURLs,
		"allow_relative_urls":                       policyAllowRelativeURLs,
		"allow_url_schemes":                         policyAllowURLSchemes,
		"require_nofollow_on_links":                 policyRequireNoFollowOnLinks,
		"require_noreferrer_on_links":               policyRequireNoReferrerOnLinks,
		"add_target_blank_to_fully_qualified_links": policyAddTargetBlankToFullyQualifiedLinks,
		"allow_data_uri_images":                     policyAllowDataURIImages,
		"allow_standard_attributes":                 policyAllowStandardAttributes,
		"allow_images":                              policyAllowImages,
		"allow_lists":                               policyAllowLists,
		"allow_tables":                              policyAllowTables,
		"sanitize":                                  policySanitize,
	})
}

func policyAllowElements(l *lua.LState) int {
	wrapper := checkPolicy(l)

	elements := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		elements = append(elements, l.CheckString(i))
	}

	wrapper.policy.AllowElements(elements...)

	l.Push(l.Get(1))
	return 1
}

func policyAllowAttrs(l *lua.LState) int {
	wrapper := checkPolicy(l)

	attrs := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		attrs = append(attrs, l.CheckString(i))
	}

	builder := &AttrBuilder{
		policy:   wrapper.policy,
		policyUD: l.Get(1).(*lua.LUserData), // Store original policy userdata
		attrs:    attrs,
	}

	ud := l.NewUserData()
	ud.Value = builder
	ud.Metatable = value.GetTypeMetatable(l, attrBuilderMetatable)

	l.Push(ud)
	return 1
}

func checkAttrBuilder(l *lua.LState) *AttrBuilder {
	ud := l.CheckUserData(1)
	if builder, ok := ud.Value.(*AttrBuilder); ok {
		return builder
	}
	l.ArgError(1, "expected AttrBuilder")
	return nil
}

func registerAttrBuilder(l *lua.LState) {
	value.RegisterTypeMethods(l, attrBuilderMetatable, nil, map[string]lua.LGFunction{
		"on_elements": attrBuilderOnElements,
		"globally":    attrBuilderGlobally,
		"matching":    attrBuilderMatching,
	})
}

func attrBuilderOnElements(l *lua.LState) int {
	builder := checkAttrBuilder(l)

	elements := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		elements = append(elements, l.CheckString(i))
	}

	if builder.regex != nil {
		builder.policy.AllowAttrs(builder.attrs...).Matching(builder.regex).OnElements(elements...)
	} else {
		builder.policy.AllowAttrs(builder.attrs...).OnElements(elements...)
	}

	l.Push(builder.policyUD) // Return original policy userdata
	return 1
}

func attrBuilderGlobally(l *lua.LState) int {
	builder := checkAttrBuilder(l)

	if builder.regex != nil {
		builder.policy.AllowAttrs(builder.attrs...).Matching(builder.regex).Globally()
	} else {
		builder.policy.AllowAttrs(builder.attrs...).Globally()
	}

	l.Push(builder.policyUD) // Return original policy userdata
	return 1
}

func attrBuilderMatching(l *lua.LState) int {
	builder := checkAttrBuilder(l)
	pattern := l.CheckString(2)

	regex, err := regexp.Compile(pattern)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newHTMLRegexError(l, err, pattern))
		return 2
	}

	builder.regex = regex

	l.Push(l.Get(1)) // Return AttrBuilder for continued chaining
	return 1
}

func policyAllowStandardURLs(l *lua.LState) int {
	wrapper := checkPolicy(l)
	wrapper.policy.AllowStandardURLs()
	l.Push(l.Get(1))
	return 1
}

func policyRequireParseableURLs(l *lua.LState) int {
	wrapper := checkPolicy(l)
	require := l.CheckBool(2)
	wrapper.policy.RequireParseableURLs(require)
	l.Push(l.Get(1))
	return 1
}

func policyAllowRelativeURLs(l *lua.LState) int {
	wrapper := checkPolicy(l)
	allow := l.CheckBool(2)
	wrapper.policy.AllowRelativeURLs(allow)
	l.Push(l.Get(1))
	return 1
}

func policyAllowURLSchemes(l *lua.LState) int {
	wrapper := checkPolicy(l)

	schemes := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		schemes = append(schemes, l.CheckString(i))
	}

	wrapper.policy.AllowURLSchemes(schemes...)
	l.Push(l.Get(1))
	return 1
}

func policyRequireNoFollowOnLinks(l *lua.LState) int {
	wrapper := checkPolicy(l)
	require := l.CheckBool(2)
	wrapper.policy.RequireNoFollowOnLinks(require)
	l.Push(l.Get(1))
	return 1
}

func policyRequireNoReferrerOnLinks(l *lua.LState) int {
	wrapper := checkPolicy(l)
	require := l.CheckBool(2)
	wrapper.policy.RequireNoReferrerOnLinks(require)
	l.Push(l.Get(1))
	return 1
}

func policyAddTargetBlankToFullyQualifiedLinks(l *lua.LState) int {
	wrapper := checkPolicy(l)
	add := l.CheckBool(2)
	wrapper.policy.AddTargetBlankToFullyQualifiedLinks(add)
	l.Push(l.Get(1))
	return 1
}

func policyAllowDataURIImages(l *lua.LState) int {
	wrapper := checkPolicy(l)
	wrapper.policy.AllowDataURIImages()
	l.Push(l.Get(1))
	return 1
}

func policyAllowStandardAttributes(l *lua.LState) int {
	wrapper := checkPolicy(l)
	wrapper.policy.AllowStandardAttributes()
	l.Push(l.Get(1))
	return 1
}

func policyAllowImages(l *lua.LState) int {
	wrapper := checkPolicy(l)
	wrapper.policy.AllowImages()
	l.Push(l.Get(1))
	return 1
}

func policyAllowLists(l *lua.LState) int {
	wrapper := checkPolicy(l)
	wrapper.policy.AllowLists()
	l.Push(l.Get(1))
	return 1
}

func policyAllowTables(l *lua.LState) int {
	wrapper := checkPolicy(l)
	wrapper.policy.AllowTables()
	l.Push(l.Get(1))
	return 1
}

func policySanitize(l *lua.LState) int {
	wrapper := checkPolicy(l)
	input := l.CheckString(2)

	result := wrapper.policy.Sanitize(input)

	l.Push(lua.LString(result))
	return 1
}
