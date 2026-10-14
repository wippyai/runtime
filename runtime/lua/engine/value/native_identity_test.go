// SPDX-License-Identifier: MPL-2.0

package value

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestRegisteredNativeComparisonAndFunctionKeys(t *testing.T) {
	name := t.Name()
	defer metatableRegistry.Delete(name)
	compare := lua.LGoFunc(func(L *lua.LState) int { L.Push(lua.LTrue); return 1 })
	method := lua.LGoFunc(func(L *lua.LState) int { L.Push(lua.LNumber(42)); return 1 })
	mt := RegisterTypeMethods(nil, name, map[string]lua.LGoFunc{
		"__eq": compare, "__lt": compare, "__le": compare,
	}, map[string]lua.LGoFunc{"answer": method})
	// Runtime metatables are immutable and shared across independently owned states.
	for i := 0; i < 2; i++ {
		func() {
			L := lua.NewState()
			defer L.Close()
			for _, global := range []string{"a", "b"} {
				object := L.NewUserData()
				L.SetMetatable(object, mt)
				L.SetGlobal(global, object)
			}
			if err := L.DoString(`
    assert(a==b and a<b and a<=b)
    local handlers={}
    handlers[a.answer]="answer"
    assert(handlers[b.answer]=="answer")
    local count=0
    for fn,name in pairs(handlers) do
     assert(rawequal(fn,a.answer) and fn()==42 and name=="answer")
     handlers[fn]=nil
     count=count+1
    end
    assert(count==1 and next(handlers)==nil)
   `); err != nil {
				t.Fatalf("state %d: %v", i, err)
			}
		}()
	}
}
