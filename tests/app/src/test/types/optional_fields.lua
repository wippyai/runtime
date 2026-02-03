type Config = {
	host: string,
	port: number,
	timeout?: number,
	debug?: boolean
}

local function make_config(host: string, port: number): Config
	return { host = host, port = port }
end

local function get_timeout(cfg: Config): number
	return cfg.timeout or 30
end

local function main(): boolean
	local cfg1: Config = make_config("localhost", 8080)
	local cfg2: Config = { host = "example.com", port = 443, timeout = 60, debug = true }

	local t1: number = get_timeout(cfg1)
	local t2: number = get_timeout(cfg2)

	return cfg1.host == "localhost" and cfg1.port == 8080 and
	t1 == 30 and t2 == 60 and cfg2.debug == true
end

return { main = main }
