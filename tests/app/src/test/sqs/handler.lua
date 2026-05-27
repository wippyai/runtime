-- SPDX-License-Identifier: MPL-2.0

local logger = require("logger")
local queue = require("queue")
local store = require("store")

local function main(body)
	local msg, err = queue.message()
	if err then
		logger:error("failed to get message", { error = tostring(err) })
		return false
	end

	local msg_id = msg:id()
	local correlation_id = msg:header("correlation_id")

	local s, store_err = store.get("app.test.store:memory")
	if store_err then
		logger:error("failed to get store", { error = tostring(store_err) })
		return false
	end

	-- settle-coordination probe on SQS: exercise the single-shot
	-- ack/nack contract. SQS ack = DeleteMessage, nack =
	-- ChangeMessageVisibility. The first call settles; the second
	-- must return an INVALID error instead of a second broker call.
	if type(body) == "table" and body.action == "double_settle_probe" then
		local first_ok, first_err = msg:ack()
		local second_ok, second_err = msg:ack()
		local nack_ok, nack_err = msg:nack()

		s:set("sqs:settle_probe:" .. correlation_id, {
			correlation_id  = correlation_id,
			first_ok        = first_ok,
			first_err_nil   = first_err == nil,
			second_ok       = second_ok,
			second_err_kind = second_err and second_err:kind() or nil,
			nack_ok         = nack_ok,
			nack_err_kind   = nack_err and nack_err:kind() or nil,
		}, 300)
		return true
	end

	-- Default: record everything we can observe so tests can assert on
	-- round-trip body + headers.
	local result = {
		msg_id         = msg_id,
		correlation_id = correlation_id,
		body           = body,
		headers        = msg:headers(),
	}

	s:set("sqs:processed:" .. msg_id, result, 300)
	if correlation_id and correlation_id ~= "" then
		s:set("sqs:by_corr:" .. correlation_id, result, 300)
	end

	return true
end

return { main = main }
