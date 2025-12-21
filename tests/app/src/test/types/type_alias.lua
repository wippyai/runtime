type Point = {x: number, y: number}
type Name = string
type Handler = (string) -> string

local function distance(p1: Point, p2: Point): number
    local dx: number = p2.x - p1.x
    local dy: number = p2.y - p1.y
    return math.sqrt(dx * dx + dy * dy)
end

local function greet(name: Name): string
    return "Hello, " .. name
end

local function apply(handler: Handler, input: string): string
    return handler(input)
end

local function main(): boolean
    local origin: Point = { x = 0, y = 0 }
    local target: Point = { x = 3, y = 4 }

    local d: number = distance(origin, target)
    local msg: string = greet("world")
    local result: string = apply(function(s: string): string return s:upper() end, "test")

    return d == 5 and msg == "Hello, world" and result == "TEST"
end

return { main = main }
