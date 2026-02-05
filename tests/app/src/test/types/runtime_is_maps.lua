local assert = require("assert2")

type Score = number @min(0) @max(100)
type Scores = {[string]: Score}
type Profile = {name: string @min_len(1), scores: Scores}

local function main(): boolean
	local scores_ok, scores_err = Scores:is({math = 98, sci = 75})
	assert.not_nil(scores_ok, "Scores valid should pass")
	assert.is_nil(scores_err, "Scores valid should have nil error")

	local scores_bad, scores_bad_err = Scores:is({math = -1})
	assert.is_nil(scores_bad, "Scores below min should fail")
	assert.not_nil(scores_bad_err, "Scores below min should return error")
	assert.error_contains(scores_bad_err, "minimum", "Scores min should mention minimum")

	local scores_bad2, scores_bad2_err = Scores:is({math = 101})
	assert.is_nil(scores_bad2, "Scores above max should fail")
	assert.not_nil(scores_bad2_err, "Scores above max should return error")
	assert.error_contains(scores_bad2_err, "maximum", "Scores max should mention maximum")

	local profile_ok, profile_err = Profile:is({name = "Ada", scores = {math = 90}})
	assert.not_nil(profile_ok, "Profile valid should pass")
	assert.is_nil(profile_err, "Profile valid should have nil error")

	local profile_bad, profile_bad_err = Profile:is({name = "", scores = {math = 90}})
	assert.is_nil(profile_bad, "Profile empty name should fail")
	assert.not_nil(profile_bad_err, "Profile empty name should return error")
	assert.error_contains(profile_bad_err, "length", "Profile name min_len should mention length")

	return true
end

return { main = main }
