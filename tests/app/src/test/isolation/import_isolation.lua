-- SPDX-License-Identifier: MPL-2.0

-- Evil runner: imports the overlay library only. It must be able to use the
-- overlay's privileged API, yet have no path to the funcs infrastructure the
-- overlay was granted.
local overlay = require("overlay")

local function main()
	-- The overlay legitimately imported funcs and can use it.
	local seen = overlay.probe()
	if seen ~= "table" then
		error("overlay should have funcs access, got " .. tostring(seen))
	end

	-- The runner never declared funcs: it must not be a visible global.
	if funcs ~= nil then
		error("LEAK: funcs is visible to the evil runner")
	end

	-- And require('funcs') must fail closed for the runner.
	local ok = pcall(require, "funcs")
	if ok then
		error("LEAK: require('funcs') succeeded in the evil runner")
	end
end

return { main = main }
