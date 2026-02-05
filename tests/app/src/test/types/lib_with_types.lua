type Config = {host: string, port: number}

local function create(host: string, port: number): Config
    return {host = host, port = port}
end

return { create = create, Config = Config }
