-- SPDX-License-Identifier: MPL-2.0

local logger = require("logger")
local queue = require("queue")
local store = require("store")
local time = require("time")

local function main(body)
-- Get message metadata
	local msg, err = queue.message()
	if err then
		logger:error("failed to get message", {error = tostring(err)})
		return false
	end

	local msg_id = msg:id()
	local correlation_id = msg:header("correlation_id")

	logger:info("processing task", {
		msg_id = msg_id,
		correlation_id = correlation_id,
		body_type = type(body)
	})

	-- Get store instance
	local s, store_err = store.get("app.test.store:memory")
	if store_err then
		logger:error("failed to get store", {error = tostring(store_err)})
		return false
	end

	-- settle-coordination probe: exercise the single-shot ack/nack
	-- contract from Lua. The first settle must win; the second settle
	-- must surface a structured INVALID error. Recording the observed
	-- error shapes in the store lets the test assert them.
	if type(body) == "table" and body.action == "double_settle_probe" then
		local first_ok, first_err = msg:ack()
		local second_ok, second_err = msg:ack()
		local nack_ok, nack_err = msg:nack()

		local outcome = {
			correlation_id = correlation_id,
			first_ok = first_ok,
			first_err_nil = first_err == nil,
			second_ok = second_ok,
			second_err_kind = second_err and second_err:kind() or nil,
			second_err_msg = second_err and second_err:message() or nil,
			nack_ok = nack_ok,
			nack_err_kind = nack_err and nack_err:kind() or nil,
		}
		s:set("queue:settle_probe:" .. correlation_id, outcome, 300)
		return true
	end

	-- concurrency probe: mutual-signaling rendezvous. Each handler writes
	-- a unique "alive" sentinel (keyed by its own correlation_id) and then
	-- spins waiting for the peer's sentinel (correlation_id carried in the
	-- message body as `peer`). Per-handler keys avoid the read-modify-write
	-- race that a shared counter would have under true concurrency. Serial
	-- consumer -> handler A posts, spins to timeout without ever seeing B,
	-- overlapped=false. Concurrent -> both sentinels appear, both overlap.
	if type(body) == "table" and body.action == "concurrency_probe" then
		local batch = body.batch
		local peer  = body.peer
		local alive_key = "queue:concurrency:alive:" .. batch .. ":" .. correlation_id
		local peer_key  = "queue:concurrency:alive:" .. batch .. ":" .. peer

		s:set(alive_key, true, 300)

		local saw_peer = false
		for _ = 1, 200 do
			if s:has(peer_key) then
				saw_peer = true
				break
			end
			time.sleep("20ms")
		end

		s:set("queue:concurrency:" .. correlation_id, {
			batch       = batch,
			peer        = peer,
			overlapped  = saw_peer,
			msg_id      = msg_id,
		}, 300)
		return true
	end

	-- Store the processed message for verification
	local result_key = "queue:processed:" .. msg_id
	local result = {
		msg_id = msg_id,
		correlation_id = correlation_id,
		body = body,
		headers = msg:headers(),
		processed_at = os.time()
	}

	local _, err = s:set(result_key, result, 300)
	if err then
		logger:error("failed to store result", {error = tostring(err)})
		return false
	end

	-- Also index by correlation_id when present for test lookup.
	if correlation_id and correlation_id ~= "" then
		s:set("queue:by_corr:" .. correlation_id, result, 300)
	end

	-- Increment counter for total processed messages
	local counter_key = "queue:counter"
	local current, _ = s:get(counter_key)
	local count = (current or 0) + 1
	s:set(counter_key, count, 300)

	logger:info("task processed successfully", {
		msg_id = msg_id,
		total_processed = count
	})

	return true
end

return { main = main }
