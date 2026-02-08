local assert = require("assert2")

type Profile = {
	age: number @min(0) @max(120),
	name: string @min_len(1) @max_len(10),
	email: string @pattern("^.+@.+\\..+$"),
	note?: string @min_len(2)
}

local function main(): boolean
	local ok, err = Profile:is({
		age = 30,
		name = "Ada",
		email = "ada@example.com"
	})
	assert.not_nil(ok, "valid profile should pass")
	assert.is_nil(err, "valid profile should have nil error")

	local bad_age, age_err = Profile:is({
		age = -1,
		name = "Ada",
		email = "ada@example.com"
	})
	assert.is_nil(bad_age, "age below min should fail")
	assert.not_nil(age_err, "age below min should return error")
	assert.error_contains(age_err, "min", "error should mention min constraint")

	local bad_name, name_err = Profile:is({
		age = 30,
		name = "",
		email = "ada@example.com"
	})
	assert.is_nil(bad_name, "name too short should fail")
	assert.not_nil(name_err, "name too short should return error")
	assert.error_contains(name_err, "length", "error should mention length constraint")

	local bad_email, email_err = Profile:is({
		age = 30,
		name = "Ada",
		email = "invalid"
	})
	assert.is_nil(bad_email, "invalid email should fail")
	assert.not_nil(email_err, "invalid email should return error")
	assert.error_contains(email_err, "pattern", "error should mention pattern constraint")

	local ok_note, note_err = Profile:is({
		age = 30,
		name = "Ada",
		email = "ada@example.com",
		note = "hi"
	})
	assert.not_nil(ok_note, "optional note should pass when valid")
	assert.is_nil(note_err, "optional note should have nil error when valid")

	local bad_note, note_err2 = Profile:is({
		age = 30,
		name = "Ada",
		email = "ada@example.com",
		note = "x"
	})
	assert.is_nil(bad_note, "optional note too short should fail")
	assert.not_nil(note_err2, "optional note too short should return error")
	assert.error_contains(note_err2, "length", "error should mention length constraint")

	local ok_missing_note, missing_err = Profile:is({
		age = 30,
		name = "Ada",
		email = "ada@example.com"
	})
	assert.not_nil(ok_missing_note, "missing optional note should pass")
	assert.is_nil(missing_err, "missing optional note should have nil error")

	return true
end

return { main = main }
