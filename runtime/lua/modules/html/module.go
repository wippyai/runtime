package html

import (
	"regexp"

	"github.com/microcosm-cc/bluemonday"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const (
	typePolicy      = "html.Policy"
	typeAttrBuilder = "html.AttrBuilder"
)

// Module is the html module definition.
var Module = &luaapi.ModuleDef{
	Name:        "html",
	Description: "HTML sanitization with policy-based filtering",
	Class:       []string{luaapi.ClassSecurity, luaapi.ClassDeterministic},
	Build:       buildModule,
}

func init() {
	value.RegisterTypeMethods(nil, typePolicy, nil, map[string]lua.LGoFunc{
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

	value.RegisterTypeMethods(nil, typeAttrBuilder, nil, map[string]lua.LGoFunc{
		"on_elements": attrBuilderOnElements,
		"globally":    attrBuilderGlobally,
		"matching":    attrBuilderMatching,
	})
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 1)

	sanitizeMod := lua.CreateTable(0, 3)
	sanitizeMod.RawSetString("new_policy", lua.LGoFunc(newPolicy))
	sanitizeMod.RawSetString("ugc_policy", lua.LGoFunc(ugcPolicy))
	sanitizeMod.RawSetString("strict_policy", lua.LGoFunc(strictPolicy))
	sanitizeMod.Immutable = true
	mod.RawSetString("sanitize", sanitizeMod)

	mod.Immutable = true
	return mod, nil
}

type PolicyWrapper struct {
	policy *bluemonday.Policy
}

type AttrBuilder struct {
	policy   *bluemonday.Policy
	policyUD *lua.LUserData
	attrs    []string
	regex    *regexp.Regexp
}

func checkPolicy(l *lua.LState) *PolicyWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*PolicyWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected html.Policy")
	return nil
}

func checkAttrBuilder(l *lua.LState) *AttrBuilder {
	ud := l.CheckUserData(1)
	if builder, ok := ud.Value.(*AttrBuilder); ok {
		return builder
	}
	l.ArgError(1, "expected html.AttrBuilder")
	return nil
}

func newPolicy(l *lua.LState) int {
	policy := bluemonday.NewPolicy()
	value.PushTypedUserData(l, &PolicyWrapper{policy: policy}, typePolicy)
	l.Push(lua.LNil)
	return 2
}

func ugcPolicy(l *lua.LState) int {
	policy := bluemonday.UGCPolicy()
	value.PushTypedUserData(l, &PolicyWrapper{policy: policy}, typePolicy)
	l.Push(lua.LNil)
	return 2
}

func strictPolicy(l *lua.LState) int {
	policy := bluemonday.StrictPolicy()
	value.PushTypedUserData(l, &PolicyWrapper{policy: policy}, typePolicy)
	l.Push(lua.LNil)
	return 2
}

func policyAllowElements(l *lua.LState) int {
	wrapper := checkPolicy(l)
	if wrapper == nil {
		return 0
	}

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
	if wrapper == nil {
		return 0
	}

	attrs := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		attrs = append(attrs, l.CheckString(i))
	}

	builder := &AttrBuilder{
		policy:   wrapper.policy,
		policyUD: l.Get(1).(*lua.LUserData),
		attrs:    attrs,
	}

	value.PushTypedUserData(l, builder, typeAttrBuilder)
	return 1
}

func policyAllowStandardURLs(l *lua.LState) int {
	wrapper := checkPolicy(l)
	if wrapper == nil {
		return 0
	}
	wrapper.policy.AllowStandardURLs()
	l.Push(l.Get(1))
	return 1
}

func policyRequireParseableURLs(l *lua.LState) int {
	wrapper := checkPolicy(l)
	if wrapper == nil {
		return 0
	}
	require := l.CheckBool(2)
	wrapper.policy.RequireParseableURLs(require)
	l.Push(l.Get(1))
	return 1
}

func policyAllowRelativeURLs(l *lua.LState) int {
	wrapper := checkPolicy(l)
	if wrapper == nil {
		return 0
	}
	allow := l.CheckBool(2)
	wrapper.policy.AllowRelativeURLs(allow)
	l.Push(l.Get(1))
	return 1
}

func policyAllowURLSchemes(l *lua.LState) int {
	wrapper := checkPolicy(l)
	if wrapper == nil {
		return 0
	}

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
	if wrapper == nil {
		return 0
	}
	require := l.CheckBool(2)
	wrapper.policy.RequireNoFollowOnLinks(require)
	l.Push(l.Get(1))
	return 1
}

func policyRequireNoReferrerOnLinks(l *lua.LState) int {
	wrapper := checkPolicy(l)
	if wrapper == nil {
		return 0
	}
	require := l.CheckBool(2)
	wrapper.policy.RequireNoReferrerOnLinks(require)
	l.Push(l.Get(1))
	return 1
}

func policyAddTargetBlankToFullyQualifiedLinks(l *lua.LState) int {
	wrapper := checkPolicy(l)
	if wrapper == nil {
		return 0
	}
	add := l.CheckBool(2)
	wrapper.policy.AddTargetBlankToFullyQualifiedLinks(add)
	l.Push(l.Get(1))
	return 1
}

func policyAllowDataURIImages(l *lua.LState) int {
	wrapper := checkPolicy(l)
	if wrapper == nil {
		return 0
	}
	wrapper.policy.AllowDataURIImages()
	l.Push(l.Get(1))
	return 1
}

func policyAllowStandardAttributes(l *lua.LState) int {
	wrapper := checkPolicy(l)
	if wrapper == nil {
		return 0
	}
	wrapper.policy.AllowStandardAttributes()
	l.Push(l.Get(1))
	return 1
}

func policyAllowImages(l *lua.LState) int {
	wrapper := checkPolicy(l)
	if wrapper == nil {
		return 0
	}
	wrapper.policy.AllowImages()
	l.Push(l.Get(1))
	return 1
}

func policyAllowLists(l *lua.LState) int {
	wrapper := checkPolicy(l)
	if wrapper == nil {
		return 0
	}
	wrapper.policy.AllowLists()
	l.Push(l.Get(1))
	return 1
}

func policyAllowTables(l *lua.LState) int {
	wrapper := checkPolicy(l)
	if wrapper == nil {
		return 0
	}
	wrapper.policy.AllowTables()
	l.Push(l.Get(1))
	return 1
}

func policySanitize(l *lua.LState) int {
	wrapper := checkPolicy(l)
	if wrapper == nil {
		return 0
	}
	input := l.CheckString(2)
	result := wrapper.policy.Sanitize(input)
	l.Push(lua.LString(result))
	return 1
}

func attrBuilderOnElements(l *lua.LState) int {
	builder := checkAttrBuilder(l)
	if builder == nil {
		return 0
	}

	elements := make([]string, 0, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		elements = append(elements, l.CheckString(i))
	}

	if builder.regex != nil {
		builder.policy.AllowAttrs(builder.attrs...).Matching(builder.regex).OnElements(elements...)
	} else {
		builder.policy.AllowAttrs(builder.attrs...).OnElements(elements...)
	}

	l.Push(builder.policyUD)
	return 1
}

func attrBuilderGlobally(l *lua.LState) int {
	builder := checkAttrBuilder(l)
	if builder == nil {
		return 0
	}

	if builder.regex != nil {
		builder.policy.AllowAttrs(builder.attrs...).Matching(builder.regex).Globally()
	} else {
		builder.policy.AllowAttrs(builder.attrs...).Globally()
	}

	l.Push(builder.policyUD)
	return 1
}

func attrBuilderMatching(l *lua.LState) int {
	builder := checkAttrBuilder(l)
	if builder == nil {
		return 0
	}
	pattern := l.CheckString(2)

	regex, err := regexp.Compile(pattern)
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "regex compile error").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	builder.regex = regex
	l.Push(l.Get(1))
	l.Push(lua.LNil)
	return 2
}
