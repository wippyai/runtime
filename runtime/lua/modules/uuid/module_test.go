// SPDX-License-Identifier: MPL-2.0

package uuid

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	mod := l.GetGlobal("uuid")
	if mod.Type() != lua.LTTable {
		t.Fatal("uuid module not registered")
	}

	modTbl := mod.(*lua.LTable)
	funcs := []string{"v1", "v3", "v4", "v5", "v7", "validate", "version", "variant", "parse", "format"}
	for _, fn := range funcs {
		if modTbl.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("%s function not registered", fn)
		}
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

	mod1 := l1.GetGlobal("uuid").(*lua.LTable)
	mod2 := l2.GetGlobal("uuid").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestUUIDV1(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local id, err = uuid.v1()
		if not id then error(tostring(err)) end
		if #id ~= 36 then error("invalid uuid length") end
	`)
	if err != nil {
		t.Errorf("v1 test failed: %v", err)
	}
}

func TestUUIDV3(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local ns = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
		local id, err = uuid.v3(ns, "test")
		if not id then error(tostring(err)) end
		if #id ~= 36 then error("invalid uuid length") end
	`)
	if err != nil {
		t.Errorf("v3 test failed: %v", err)
	}
}

func TestUUIDV3InvalidNamespace(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local id, err = uuid.v3("invalid", "test")
		if id ~= nil then error("expected nil") end
		if err == nil then error("expected error") end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind, got: " .. tostring(err:kind()))
		end
		if err:retryable() ~= false then
			error("expected retryable to be false")
		end
	`)
	if err != nil {
		t.Errorf("v3 invalid namespace test failed: %v", err)
	}
}

func TestUUIDV3MissingArgs(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local id, err = uuid.v3()
		if id ~= nil then error("expected nil") end
		if err == nil then error("expected error") end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("v3 missing args test failed: %v", err)
	}
}

func TestUUIDV4(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local id, err = uuid.v4()
		if not id then error(tostring(err)) end
		if #id ~= 36 then error("invalid uuid length") end
	`)
	if err != nil {
		t.Errorf("v4 test failed: %v", err)
	}
}

func TestUUIDV5(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local ns = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
		local id, err = uuid.v5(ns, "test")
		if not id then error(tostring(err)) end
		if #id ~= 36 then error("invalid uuid length") end
	`)
	if err != nil {
		t.Errorf("v5 test failed: %v", err)
	}
}

func TestUUIDV5InvalidNamespace(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local id, err = uuid.v5("invalid", "test")
		if id ~= nil then error("expected nil") end
		if err == nil then error("expected error") end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("v5 invalid namespace test failed: %v", err)
	}
}

func TestUUIDV7(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local id, err = uuid.v7()
		if not id then error(tostring(err)) end
		if #id ~= 36 then error("invalid uuid length") end
	`)
	if err != nil {
		t.Errorf("v7 test failed: %v", err)
	}
}

func TestValidate(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local valid, _ = uuid.validate("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
		if not valid then error("should be valid") end

		local invalid, _ = uuid.validate("not-a-uuid")
		if invalid then error("should be invalid") end

		local nonstring, _ = uuid.validate(123)
		if nonstring then error("non-string should be invalid") end
	`)
	if err != nil {
		t.Errorf("validate test failed: %v", err)
	}
}

func TestVersion(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local ver, err = uuid.version("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
		if not ver then error(tostring(err)) end
		if ver ~= 1 then error("expected version 1") end
	`)
	if err != nil {
		t.Errorf("version test failed: %v", err)
	}
}

func TestVersionInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local ver, err = uuid.version("invalid")
		if ver ~= nil then error("expected nil") end
		if err == nil then error("expected error") end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind, got: " .. tostring(err:kind()))
		end

		ver, err = uuid.version(123)
		if ver ~= nil then error("expected nil for non-string") end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind for non-string")
		end
	`)
	if err != nil {
		t.Errorf("version invalid test failed: %v", err)
	}
}

func TestVariant(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local var, err = uuid.variant("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
		if not var then error(tostring(err)) end
		if var ~= "RFC4122" then error("expected RFC4122 variant") end
	`)
	if err != nil {
		t.Errorf("variant test failed: %v", err)
	}
}

func TestVariantInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local var, err = uuid.variant("invalid")
		if var ~= nil then error("expected nil") end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind")
		end

		var, err = uuid.variant(123)
		if var ~= nil then error("expected nil for non-string") end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind for non-string")
		end
	`)
	if err != nil {
		t.Errorf("variant invalid test failed: %v", err)
	}
}

func TestParse(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local info, err = uuid.parse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
		if not info then error(tostring(err)) end
		if info.version ~= 1 then error("expected version 1") end
		if info.variant ~= "RFC4122" then error("expected RFC4122") end
		if not info.timestamp then error("expected timestamp") end
	`)
	if err != nil {
		t.Errorf("parse test failed: %v", err)
	}
}

func TestParseV7(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local id, _ = uuid.v7()
		local info, err = uuid.parse(id)
		if not info then error(tostring(err)) end
		if info.version ~= 7 then error("expected version 7") end
	`)
	if err != nil {
		t.Errorf("parse v7 test failed: %v", err)
	}
}

func TestParseInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local info, err = uuid.parse("invalid")
		if info ~= nil then error("expected nil") end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind")
		end

		info, err = uuid.parse(123)
		if info ~= nil then error("expected nil for non-string") end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind for non-string")
		end
	`)
	if err != nil {
		t.Errorf("parse invalid test failed: %v", err)
	}
}

func TestFormat(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local id = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"

		local std, _ = uuid.format(id, "standard")
		if std ~= id then error("standard format mismatch") end

		local simple, _ = uuid.format(id, "simple")
		if simple ~= "6ba7b8109dad11d180b400c04fd430c8" then error("simple format mismatch") end

		local urn, _ = uuid.format(id, "urn")
		if urn ~= "urn:uuid:" .. id then error("urn format mismatch") end

		local default, _ = uuid.format(id)
		if default ~= id then error("default format mismatch") end
	`)
	if err != nil {
		t.Errorf("format test failed: %v", err)
	}
}

func TestFormatInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local result, err = uuid.format("invalid")
		if result ~= nil then error("expected nil for invalid uuid") end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind")
		end

		result, err = uuid.format("6ba7b810-9dad-11d1-80b4-00c04fd430c8", "unknown")
		if result ~= nil then error("expected nil for unknown format") end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind for unknown format")
		end

		result, err = uuid.format(123)
		if result ~= nil then error("expected nil for non-string") end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID kind for non-string")
		end
	`)
	if err != nil {
		t.Errorf("format invalid test failed: %v", err)
	}
}

func TestV4Uniqueness(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local uuids = {}
		for i = 1, 100 do
			local id, err = uuid.v4()
			if not id then error(tostring(err)) end
			if uuids[id] then error("duplicate uuid generated") end
			uuids[id] = true
		end
	`)
	if err != nil {
		t.Errorf("v4 uniqueness test failed: %v", err)
	}
}

func TestV3V5Determinism(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local ns = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"

		local v3_1, _ = uuid.v3(ns, "test")
		local v3_2, _ = uuid.v3(ns, "test")
		if v3_1 ~= v3_2 then error("v3 should be deterministic") end

		local v5_1, _ = uuid.v5(ns, "test")
		local v5_2, _ = uuid.v5(ns, "test")
		if v5_1 ~= v5_2 then error("v5 should be deterministic") end

		if v3_1 == v5_1 then error("v3 and v5 should produce different results") end
	`)
	if err != nil {
		t.Errorf("v3/v5 determinism test failed: %v", err)
	}
}
