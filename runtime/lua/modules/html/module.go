package html

import (
	"regexp"
	"sync"

	"github.com/microcosm-cc/bluemonday"
	lua2api "github.com/wippyai/runtime/api/runtime/lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

const (
	policyMetatable      = "html.Policy"
	attrBuilderMetatable = "html.AttrBuilder"
)

var (
	moduleTable   *lua.LTable
	policyMT      *lua.LTable
	attrBuilderMT *lua.LTable
	registration  *lua2api.Registration
	initOnce      sync.Once
)

// Module is the singleton html module instance.
var Module = &htmlModule{}

type htmlModule struct{}

func (m *htmlModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "html",
		Description: "HTML sanitization with policy-based filtering",
		Class:       []string{luaapi.ClassSecurity, luaapi.ClassDeterministic},
	}
}

func (m *htmlModule) Register(l *lua.LState) *lua2api.Registration {
	initOnce.Do(func() {
		policyMT = createPolicyMetatable(l)
		attrBuilderMT = createAttrBuilderMetatable(l)

		sanitizeMod := &lua.LTable{}
		sanitizeMod.RawSetString("new_policy", lua.LGoFunc(newPolicy))
		sanitizeMod.RawSetString("ugc_policy", lua.LGoFunc(ugcPolicy))
		sanitizeMod.RawSetString("strict_policy", lua.LGoFunc(strictPolicy))
		sanitizeMod.Immutable = true

		mod := &lua.LTable{}
		mod.RawSetString("sanitize", sanitizeMod)
		mod.Immutable = true
		moduleTable = mod

		registration = &lua2api.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})

	l.SetField(l.Get(lua.RegistryIndex), policyMetatable, policyMT)
	l.SetField(l.Get(lua.RegistryIndex), attrBuilderMetatable, attrBuilderMT)

	return registration
}

func (m *htmlModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use lua2api.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	lua2api.LoadModule(l, Module)
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

func getPolicyMT(l *lua.LState) lua.LValue {
	return l.GetField(l.Get(lua.RegistryIndex), policyMetatable)
}

func getAttrBuilderMT(l *lua.LState) lua.LValue {
	return l.GetField(l.Get(lua.RegistryIndex), attrBuilderMetatable)
}

func createPolicyMetatable(l *lua.LState) *lua.LTable {
	mt := l.CreateTable(0, 2)

	index := l.CreateTable(0, 14)
	index.RawSetString("allow_elements", lua.LGoFunc(policyAllowElements))
	index.RawSetString("allow_attrs", lua.LGoFunc(policyAllowAttrs))
	index.RawSetString("allow_standard_urls", lua.LGoFunc(policyAllowStandardURLs))
	index.RawSetString("require_parseable_urls", lua.LGoFunc(policyRequireParseableURLs))
	index.RawSetString("allow_relative_urls", lua.LGoFunc(policyAllowRelativeURLs))
	index.RawSetString("allow_url_schemes", lua.LGoFunc(policyAllowURLSchemes))
	index.RawSetString("require_nofollow_on_links", lua.LGoFunc(policyRequireNoFollowOnLinks))
	index.RawSetString("require_noreferrer_on_links", lua.LGoFunc(policyRequireNoReferrerOnLinks))
	index.RawSetString("add_target_blank_to_fully_qualified_links", lua.LGoFunc(policyAddTargetBlankToFullyQualifiedLinks))
	index.RawSetString("allow_data_uri_images", lua.LGoFunc(policyAllowDataURIImages))
	index.RawSetString("allow_standard_attributes", lua.LGoFunc(policyAllowStandardAttributes))
	index.RawSetString("allow_images", lua.LGoFunc(policyAllowImages))
	index.RawSetString("allow_lists", lua.LGoFunc(policyAllowLists))
	index.RawSetString("allow_tables", lua.LGoFunc(policyAllowTables))
	index.RawSetString("sanitize", lua.LGoFunc(policySanitize))
	index.Immutable = true

	mt.RawSetString("__index", index)
	mt.Immutable = true
	return mt
}

func createAttrBuilderMetatable(l *lua.LState) *lua.LTable {
	mt := l.CreateTable(0, 2)

	index := l.CreateTable(0, 3)
	index.RawSetString("on_elements", lua.LGoFunc(attrBuilderOnElements))
	index.RawSetString("globally", lua.LGoFunc(attrBuilderGlobally))
	index.RawSetString("matching", lua.LGoFunc(attrBuilderMatching))
	index.Immutable = true

	mt.RawSetString("__index", index)
	mt.Immutable = true
	return mt
}

func checkPolicy(l *lua.LState) *PolicyWrapper {
	ud := l.CheckUserData(1)
	if wrapper, ok := ud.Value.(*PolicyWrapper); ok {
		return wrapper
	}
	l.ArgError(1, "expected Policy")
	return nil
}

func checkAttrBuilder(l *lua.LState) *AttrBuilder {
	ud := l.CheckUserData(1)
	if builder, ok := ud.Value.(*AttrBuilder); ok {
		return builder
	}
	l.ArgError(1, "expected AttrBuilder")
	return nil
}

func newPolicy(l *lua.LState) int {
	policy := bluemonday.NewPolicy()

	ud := l.NewUserData()
	ud.Value = &PolicyWrapper{policy: policy}
	ud.Metatable = getPolicyMT(l)

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

func ugcPolicy(l *lua.LState) int {
	policy := bluemonday.UGCPolicy()

	ud := l.NewUserData()
	ud.Value = &PolicyWrapper{policy: policy}
	ud.Metatable = getPolicyMT(l)

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
}

func strictPolicy(l *lua.LState) int {
	policy := bluemonday.StrictPolicy()

	ud := l.NewUserData()
	ud.Value = &PolicyWrapper{policy: policy}
	ud.Metatable = getPolicyMT(l)

	l.Push(ud)
	l.Push(lua.LNil)
	return 2
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
		policyUD: l.Get(1).(*lua.LUserData),
		attrs:    attrs,
	}

	ud := l.NewUserData()
	ud.Value = builder
	ud.Metatable = getAttrBuilderMT(l)

	l.Push(ud)
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

	l.Push(builder.policyUD)
	return 1
}

func attrBuilderGlobally(l *lua.LState) int {
	builder := checkAttrBuilder(l)

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
	pattern := l.CheckString(2)

	regex, err := regexp.Compile(pattern)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	builder.regex = regex

	l.Push(l.Get(1))
	l.Push(lua.LNil)
	return 2
}
