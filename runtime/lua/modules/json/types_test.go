// SPDX-License-Identifier: MPL-2.0

package json

import (
	"strings"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/check/tests/testutil"
	"github.com/wippyai/go-lua/compiler/parse"
)

func TestDecodeTypeNarrowsAfterErrorCheck(t *testing.T) {
	options := []testutil.Option{testutil.WithStdlib(), testutil.WithManifest("json", ModuleTypes())}
	source := `
		local json = require("json")
		type User = {id: string}
		local function read(s: string)
			local u, err = json.decode(s, User)
			if err then return end
			local id: string = u.id
			return id
		end
	`
	if result := testutil.Check(source, options...); result.HasError() {
		for _, d := range result.Errors {
			t.Log(d.Message)
		}
		t.Fatal("typed decode failed to narrow to User")
	}
	bad := strings.Replace(source, "local id: string = u.id", "local id: number = u.id", 1)
	if result := testutil.Check(bad, options...); !result.HasError() {
		t.Fatal("u.id must be string rather than any")
	}
}

func TestDecodeNullableRootSuccessIsNonNil(t *testing.T) {
	source := `
		local json = require("json")
		type MaybeUser = {id: string, nickname: string?}?
		local function read(raw: string): string?
			local user, err = json.decode(raw, MaybeUser)
			if err then return nil end
			local id: string = user.id
			return id
		end
		local value, err = json.decode("null", MaybeUser)
		assert(err ~= nil, "typed root null must return an error")
		assert(value == nil, "failed decode must return nil")
		assert(read("null") == nil, "root null must take the error branch")
		assert(read('{"id":"ok","nickname":null}') == "ok", "nested null must remain valid")
		local untyped, untypedErr = json.decode("null")
		assert(untyped == nil and untypedErr == nil, "untyped null must remain valid")
	`
	options := []testutil.Option{testutil.WithStdlib(), testutil.WithManifest("json", ModuleTypes())}
	result := testutil.CheckAndExport(source, "typed-null", options...)
	if result.HasError() {
		for _, d := range result.Errors {
			t.Log(d.Message)
		}
		t.Fatal("nullable typed decode must narrow to a non-nil result after the error check")
	}
	manifest, err := result.Manifest.Encode()
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := parse.ParseString(strings.Replace(source, `local json = require("json")`, "", 1), "typed-null")
	if err != nil {
		t.Fatal(err)
	}
	proto, err := lua.CompileWithOptions(chunk, "typed-null", lua.CompileOptions{TypeInfo: manifest})
	if err != nil {
		t.Fatal(err)
	}
	l := lua.NewState()
	defer l.Close()
	bindJSON(l)
	l.Push(l.LoadProto(proto))
	if err := l.PCall(0, 0, nil); err != nil {
		t.Fatal(err)
	}
}
