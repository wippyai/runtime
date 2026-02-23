-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local funcs = require("funcs")

local function main()
	local users = {
		{id = 1, name = "Alice", tags = {"admin", "dev"}, active = true},
		{id = 2, name = "Bob", tags = {"user"}, active = false},
	}

	local out, err = funcs.call("app.test.wasm:mapper_component_transform", users)
	assert.is_nil(err, "mapper_component_transform should not error")
	assert.is_table(out, "mapper_component_transform should return table")
	assert.eq(#out, 2, "mapper output length")

	local first = out[1]
	assert.is_table(first, "first record should be table")
	assert.eq(first.id, 1, "first.id")
	assert.eq(first.display, "Alice [admin, dev]", "first.display")
	assert.eq(first["tag-count"], 2, "first.tag-count")

	local second = out[2]
	assert.is_table(second, "second record should be table")
	assert.eq(second.id, 2, "second.id")
	assert.eq(second.display, "Bob [user]", "second.display")
	assert.eq(second["tag-count"], 1, "second.tag-count")

	local active_out, active_err = funcs.call("app.test.wasm:mapper_component_filter_active", users)
	assert.is_nil(active_err, "mapper_component_filter_active should not error")
	assert.is_table(active_out, "mapper_component_filter_active should return table")
	assert.eq(#active_out, 1, "active users output length")
	assert.eq(active_out[1].id, 1, "active user id")
	assert.eq(active_out[1].display, "Alice [admin, dev]", "active user display")

	local tags_out, tags_err = funcs.call("app.test.wasm:mapper_component_aggregate_tags", users)
	assert.is_nil(tags_err, "mapper_component_aggregate_tags should not error")
	assert.is_table(tags_out, "mapper_component_aggregate_tags should return table")
	assert.eq(#tags_out, 3, "tag aggregation count")
	assert.eq(tags_out[1], "admin", "sorted first tag")
	assert.eq(tags_out[2], "dev", "sorted second tag")
	assert.eq(tags_out[3], "user", "sorted third tag")

	return true
end

return { main = main }
