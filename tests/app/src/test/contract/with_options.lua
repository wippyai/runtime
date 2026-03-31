-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local contract = require("contract")
local store = require("store")

local function main()
	local s, err = store.get("app.test.store:memory")
	assert.is_nil(err, "store.get no error")

	local c, err = contract.get("app.test.contract:flaky")
	assert.is_nil(err, "get flaky contract no error")

	-- 1. Retry succeeds after transient failures (wrapper with_options)
	s:set("flaky_call_count", 0)

	local inst, err = c
		:with_options({retry = {max_attempts = 5, initial_delay = 1}})
		:open("app.test.contract:flaky_impl")
	assert.is_nil(err, "open with retry options no error")

	local result, err = inst:flaky_call()
	assert.is_nil(err, "flaky_call succeeds after retries")
	assert.eq(result, "success_after_3_attempts", "correct result after retry")
	assert.eq(s:get("flaky_call_count"), 3, "called 3 times via retry")

	-- 2. No options means no retry
	s:set("flaky_call_count", 0)

	local plain, err = contract.open("app.test.contract:flaky_impl")
	assert.is_nil(err, "open without options no error")

	local result2, err2 = plain:flaky_call()
	assert.not_nil(err2, "fails without retry")
	assert.is_nil(result2, "no result on failure")
	assert.eq(err2:kind(), "Unavailable", "error kind preserved")
	assert.eq(err2:retryable(), true, "error is retryable")
	assert.eq(s:get("flaky_call_count"), 1, "called once without retry")

	-- 3. Non-retryable error skips retry despite options
	s:set("permanent_fail_count", 0)

	local result3, err3 = inst:permanent_fail()
	assert.not_nil(err3, "permanent_fail returns error")
	assert.is_nil(result3, "no result on permanent failure")
	assert.eq(err3:kind(), "PermissionDenied", "non-retryable kind preserved")
	assert.eq(err3:retryable(), false, "error marked non-retryable")
	assert.eq(s:get("permanent_fail_count"), 1, "non-retryable called once")

	-- 4. Exhausted retries surface the last error
	s:set("always_fail_count", 0)

	local inst2, err = c
		:with_options({retry = {max_attempts = 3, initial_delay = 1}})
		:open("app.test.contract:flaky_impl")
	assert.is_nil(err, "open for exhausted test no error")

	local result4, err4 = inst2:always_fail()
	assert.not_nil(err4, "always_fail returns error after exhausting retries")
	assert.is_nil(result4, "no result when retries exhausted")
	assert.eq(s:get("always_fail_count"), 3, "called max_attempts times before giving up")

	-- 5. max_attempts = 1 behaves like no retry
	s:set("always_fail_count", 0)

	local inst3, err = c
		:with_options({retry = {max_attempts = 1, initial_delay = 1}})
		:open("app.test.contract:flaky_impl")
	assert.is_nil(err, "open with max_attempts=1 no error")

	local result5, err5 = inst3:always_fail()
	assert.not_nil(err5, "max_attempts=1 fails immediately")
	assert.eq(s:get("always_fail_count"), 1, "max_attempts=1 called only once")

	-- 6. Empty options table does not crash, no retry
	s:set("flaky_call_count", 0)

	local inst4, err = c
		:with_options({})
		:open("app.test.contract:flaky_impl")
	assert.is_nil(err, "open with empty options no error")

	local result6, err6 = inst4:flaky_call()
	assert.not_nil(err6, "empty options means no retry")
	assert.eq(s:get("flaky_call_count"), 1, "empty options called once")

	-- 7. contract.open direct with options as third arg
	s:set("flaky_call_count", 0)

	local inst5, err = contract.open("app.test.contract:flaky_impl", {}, {
		retry = {max_attempts = 5, initial_delay = 1}
	})
	assert.is_nil(err, "direct open with options no error")

	local result7, err7 = inst5:flaky_call()
	assert.is_nil(err7, "direct open retry succeeds")
	assert.eq(result7, "success_after_3_attempts", "direct open correct result")
	assert.eq(s:get("flaky_call_count"), 3, "direct open retried 3 times")

	-- 8. Options apply to multiple method calls on same instance
	s:set("flaky_call_count", 0)
	s:set("always_fail_count", 0)

	local inst6, err = c
		:with_options({retry = {max_attempts = 5, initial_delay = 1}})
		:open("app.test.contract:flaky_impl")
	assert.is_nil(err, "open for multi-method no error")

	local r8a, err8a = inst6:flaky_call()
	assert.is_nil(err8a, "first method retries and succeeds")
	assert.eq(r8a, "success_after_3_attempts", "first method result correct")
	assert.eq(s:get("flaky_call_count"), 3, "first method retried")

	local r8b, err8b = inst6:always_fail()
	assert.not_nil(err8b, "second method exhausts retries")
	assert.eq(s:get("always_fail_count"), 5, "second method used all 5 attempts")

	-- 9. with_options chains with with_context
	s:set("flaky_call_count", 0)

	local inst7, err = c
		:with_context({some_key = "some_value"})
		:with_options({retry = {max_attempts = 5, initial_delay = 1}})
		:open("app.test.contract:flaky_impl")
	assert.is_nil(err, "chained with_context + with_options no error")

	local r9, err9 = inst7:flaky_call()
	assert.is_nil(err9, "chained options retry works")
	assert.eq(r9, "success_after_3_attempts", "chained result correct")
	assert.eq(s:get("flaky_call_count"), 3, "chained retried correctly")

	return true
end

return { main = main }
