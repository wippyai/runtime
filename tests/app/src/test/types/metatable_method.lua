-- Reproduces E0003 "too many arguments" on metatable-based : method calls.
-- The type checker should resolve that instances created via setmetatable({}, MT)
-- can call methods defined with function MT:method() using : syntax.

local Runner = {}
Runner.__index = Runner

function Runner:greet(name: string): string
    return "hello " .. name
end

function Runner:find_items(options: any?): ({any}?, string?)
    return {}, nil
end

function Runner:run(options: any?): any
    options = options or {}
    local items, err = self:find_items(options)
    if err then
        return nil
    end
    local result = self:greet("world")
    return result
end

local function setup(id: string): any
    local self = setmetatable({}, Runner)
    self.id = id
    return self
end

local function main(): boolean
    local r = setup("test")
    local result = r:run()
    return result == "hello world"
end

return { main = main }
