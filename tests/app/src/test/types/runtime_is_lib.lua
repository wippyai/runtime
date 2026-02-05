local mylib = require("mylib")

type LocalPoint = {x: number, y: number}

local function main(): boolean
    local point, _ = LocalPoint:is({x = 1, y = 2})
    local cfg = mylib.create("localhost", 8080)
    local cfg_check, _ = mylib.Config:is(cfg)

    return point ~= nil and cfg_check ~= nil
end

return { main = main }
