local function add(a: number, b: number): number
    return a + b
end

local function greet(name: string): string
    return "Hello, " .. name
end

local function is_even(n: integer): boolean
    return n % 2 == 0
end

local function main(): boolean
    local sum: number = add(10, 20)
    local msg: string = greet("world")
    local even: boolean = is_even(4)

    return sum == 30 and msg == "Hello, world" and even == true
end

return { main = main }
