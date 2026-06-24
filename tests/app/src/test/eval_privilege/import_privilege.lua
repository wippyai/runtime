-- SPDX-License-Identifier: MPL-2.0

-- Proves eval per-import privilege end-to-end: the pricing import is granted
-- funcs and uses it (funcs.call), while the eval'd DSL on top cannot reach funcs
-- by any path. The runner itself runs under the user actor (root), so funcs is
-- allowed by policy; isolation, not policy, is what keeps it out of the DSL.
local assert = require("assert")

local function main()
	local runner = require("eval_runner")

	local res, err = runner.run({
		-- The DSL lists its own modules explicitly (no funcs), so it gets none
		-- of the funcs capability except through the privileged import.
		source = [[
			local pricing = require("pricing")
			return {
				build = function()
					return {
						quoted = pricing.quote("synthetic"),
						leaked = type(funcs) ~= "nil",
						can_require = (pcall(require, "funcs")),
					}
				end
			}
		]],
		method = "build",
		modules = { "json" },
		imports = {
			pricing = { id = "app.test.eval_privilege:pricing", modules = { "funcs" } },
		},
	})

	assert.is_nil(err, "eval run should succeed: " .. tostring(err))
	assert.not_nil(res, "eval returns a result")
	assert.eq(res.quoted, "synthetic", "import used funcs.call to produce the quote")
	assert.eq(res.leaked, false, "funcs must NOT be visible to the DSL")
	assert.eq(res.can_require, false, "DSL must NOT be able to require funcs")

	return true
end

return { main = main }
