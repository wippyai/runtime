-- SPDX-License-Identifier: MPL-2.0

type Point = {x: number, y: number}
type User = {id: string, name: string}
type OptionalAge = {age?: number}

local assert = require("assert2")

local function main(): boolean
    local point, perr = Point:is({x = 1, y = 2})
    assert.not_nil(point, "valid Point should pass")
    assert.is_nil(perr, "valid Point should have nil error")

    local bad, berr = Point:is({x = 1})
    assert.is_nil(bad, "Point missing required field should fail")
    assert.not_nil(berr, "Point missing required field should return error")
    assert.error_contains(berr, "y", "error should mention missing field")

    local user, uerr = User:is({id = "123", name = "Alice"})
    assert.not_nil(user, "valid User should pass")
    assert.is_nil(uerr, "valid User should have nil error")

    local opt, oerr = OptionalAge:is({})
    assert.not_nil(opt, "optional field missing should still pass")
    assert.is_nil(oerr, "optional field missing should have nil error")

    return true
end

return { main = main }
