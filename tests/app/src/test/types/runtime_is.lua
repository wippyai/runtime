type Point = {x: number, y: number}
type User = {id: string, name: string}
type OptionalAge = {age?: number}

local function main(): boolean
    local point, _ = Point:is({x = 1, y = 2})
    local bad, _ = Point:is({x = 1})
    local user, _ = User:is({id = "123", name = "Alice"})
    local opt, _ = OptionalAge:is({})

    return point ~= nil and bad == nil and user ~= nil and opt ~= nil
end

return { main = main }
