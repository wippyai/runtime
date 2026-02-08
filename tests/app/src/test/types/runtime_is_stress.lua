local assert = require("assert2")

type Point = {x: number, y: number}
type Score = number @min(0) @max(100)
type Name = string @min_len(2) @max_len(8)
type Email = string @pattern("^.+@.+\\..+$")
type TagList = {string}
type User = {name: Name, score: Score, email: Email, tags?: {string} @min_len(1) @max_len(3)}
type Id = number | string

type Case = {t: any, v: any, ok: boolean, err?: string, name: string}

local function main(): boolean
	local cases: {Case} = {
		{name = "point_ok", t = Point, v = {x = 1, y = 2}, ok = true},
		{name = "point_missing", t = Point, v = {x = 1}, ok = false, err = "y"},
		{name = "score_ok", t = Score, v = 99, ok = true},
		{name = "score_low", t = Score, v = -1, ok = false, err = "minimum"},
		{name = "score_high", t = Score, v = 101, ok = false, err = "maximum"},
		{name = "name_short", t = Name, v = "a", ok = false, err = "length"},
		{name = "email_bad", t = Email, v = "nope", ok = false, err = "pattern"},
		{name = "tags_ok", t = TagList, v = {"a", "b"}, ok = true},
		{name = "id_ok_num", t = Id, v = 1, ok = true},
		{name = "id_ok_str", t = Id, v = "x", ok = true},
		{name = "id_bad", t = Id, v = true, ok = false}
	}

	for _, c in ipairs(cases) do
		local val, err = c.t:is(c.v)
		if c.ok then
			assert.not_nil(val, c.name .. " should pass")
			assert.is_nil(err, c.name .. " should have nil error")
		else
			assert.is_nil(val, c.name .. " should fail")
			assert.not_nil(err, c.name .. " should return error")
			if c.err ~= nil then
				assert.error_contains(err, c.err, c.name .. " error should mention " .. c.err)
			end
		end
	end

	local user_ok, user_err = User:is({
		name = "Ada",
		score = 100,
		email = "ada@example.com",
		tags = {"a"}
	})
	assert.not_nil(user_ok, "User with tags should pass")
	assert.is_nil(user_err, "User with tags should have nil error")

	local user_ok2, user_err2 = User:is({
		name = "Ada",
		score = 0,
		email = "ada@example.com"
	})
	assert.not_nil(user_ok2, "User without optional tags should pass")
	assert.is_nil(user_err2, "User without optional tags should have nil error")

	local user_bad, user_bad_err = User:is({
		name = "A",
		score = 50,
		email = "ada@example.com"
	})
	assert.is_nil(user_bad, "User with short name should fail")
	assert.not_nil(user_bad_err, "User with short name should return error")
	assert.error_contains(user_bad_err, "length", "User name error should mention length")

	local user_bad_tags, user_bad_tags_err = User:is({
		name = "Ada",
		score = 50,
		email = "ada@example.com",
		tags = {}
	})
	assert.is_nil(user_bad_tags, "User with empty tags should fail")
	assert.not_nil(user_bad_tags_err, "User with empty tags should return error")
	assert.error_contains(user_bad_tags_err, "length", "User tags error should mention length")

	return true
end

return { main = main }
