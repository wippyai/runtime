// SPDX-License-Identifier: MPL-2.0

package hash

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

func newStreamState(t *testing.T) *lua.LState {
	t.Helper()
	l := lua.NewState()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
	return l
}

func TestStreamMatchesOneShot(t *testing.T) {
	l := newStreamState(t)
	defer l.Close()
	err := l.DoString(`
		for _, algorithm in ipairs({"md5", "sha1", "sha256", "sha512"}) do
			local hasher, err = hash.new(algorithm)
			if err ~= nil then error(algorithm .. ": " .. tostring(err)) end
			assert(hasher:update("hel"))
			assert(hasher:update(""))
			assert(hasher:update("lo"))
			local streamed = hasher:sum()
			local whole = hash[algorithm]("hello")
			if streamed ~= whole then error(algorithm .. " stream " .. streamed .. " differs from one-shot " .. whole) end
			local raw = hasher:sum(true)
			if #raw * 2 ~= #whole then error(algorithm .. " raw length") end
			assert(hasher:update("!"))
			if hasher:sum() ~= hash[algorithm]("hello!") then error(algorithm .. " sum did not keep the state") end
			hasher:reset()
			if hasher:sum() ~= hash[algorithm]("") then error(algorithm .. " reset did not clear the state") end
		end
	`)
	if err != nil {
		t.Fatalf("stream test failed: %v", err)
	}
}

func TestStreamErrors(t *testing.T) {
	l := newStreamState(t)
	defer l.Close()
	err := l.DoString(`
		local hasher, err = hash.new("crc32")
		if hasher ~= nil or err == nil then error("unsupported algorithm accepted") end
		local _, kind_err = hash.new(42)
		if kind_err == nil then error("non-string algorithm accepted") end
		local sha = assert(hash.new("sha256"))
		local ok, update_err = sha:update(7)
		if ok or update_err == nil then error("non-string data accepted") end
		if tostring(sha) ~= "hash.Hasher(sha256)" then error("tostring: " .. tostring(sha)) end
	`)
	if err != nil {
		t.Fatalf("stream error test failed: %v", err)
	}
}

func TestStreamLargeInput(t *testing.T) {
	l := newStreamState(t)
	defer l.Close()
	err := l.DoString(`
		local chunk = string.rep("x", 65536)
		local hasher = assert(hash.new("sha256"))
		for _ = 1, 64 do assert(hasher:update(chunk)) end
		if hasher:sum() ~= hash.sha256(string.rep(chunk, 64)) then error("large stream differs from one-shot") end
	`)
	if err != nil {
		t.Fatalf("large stream test failed: %v", err)
	}
}
