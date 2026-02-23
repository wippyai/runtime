-- SPDX-License-Identifier: MPL-2.0

-- Stress case: worker pipeline where methods pass dynamic payloads (any)
-- through multiple self:method(...) hops.

local Worker = {}
Worker.__index = Worker

function Worker:prepare(payload: any): (any, string?)
    return { prepared = payload }, nil
end

function Worker:dispatch(payload: any): (boolean, string?)
    local prepared, err = self:prepare(payload)
    if err then
        return false, err
    end
    return prepared ~= nil, nil
end

function Worker:run(payload: any): boolean
    local ok, err = self:dispatch(payload)
    if err then
        return false
    end
    return ok
end

local function new_worker(): any
    return setmetatable({}, Worker)
end

local function main(): boolean
    local worker = new_worker()
    return worker:run({ task = "sync" }) == true
end

return { main = main }
