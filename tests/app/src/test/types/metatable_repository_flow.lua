-- Stress case: repository object calling another method with any? options.
-- Common in real apps where option bags are partially typed.

local UserRepo = {}
UserRepo.__index = UserRepo

function UserRepo:find(options: any?): ({any}?, string?)
    if options and options.fail then
        return nil, "query failed"
    end
    return { { id = "u1" } }, nil
end

function UserRepo:first(options: any?): any?
    local rows, err = self:find(options)
    if err or not rows or #rows == 0 then
        return nil
    end
    return rows[1]
end

local function new_repo(): any
    return setmetatable({}, UserRepo)
end

local function main(): boolean
    local repo = new_repo()
    local user = repo:first({ limit = 1 })
    return user ~= nil
end

return { main = main }
