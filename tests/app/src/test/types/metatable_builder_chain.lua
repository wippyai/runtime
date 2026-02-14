-- Stress case: fluent builder methods using metatable + colon dispatch.
-- This should type-check without arity errors on self:execute(opts).

local QueryBuilder = {}
QueryBuilder.__index = QueryBuilder

function QueryBuilder:with_filter(filter: any): any
    self.filter = filter
    return self
end

function QueryBuilder:execute(options: any): ({string}, string?)
    local out = { "row" }
    if options and options.limit then
        out[2] = tostring(options.limit)
    end
    return out, nil
end

function QueryBuilder:run(filter: any): number
    local first, err = self:execute({ limit = 10 })
    if err then
        return 0
    end

    local chained = self:with_filter(filter)
    local second, err2 = chained:execute({ limit = 5 })
    if err2 then
        return 0
    end

    return #first + #second
end

local function new_builder(): any
    return setmetatable({}, QueryBuilder)
end

local function main(): boolean
    local builder = new_builder()
    local count = builder:run({ kind = "active" })
    return count == 4
end

return { main = main }
