-- SPDX-License-Identifier: MPL-2.0

-- Smoke test: queue.publish against a real SQS endpoint (ElasticMQ).
-- Proves the driver's publish path — Lua->Message->MarshalBody->SendMessage
-- — reaches the broker without error. Consumption is covered separately.

local assert = require("assert2")
local queue = require("queue")

local function main()
	local ok, err = queue.publish("app.test.sqs:tasks", {
		action = "sqs_publish_smoke",
		note   = "hello from sqs publish test",
	})
	assert.is_nil(err, "publish should succeed")
	assert.eq(ok, true, "publish should return true")

	return true
end

return { main = main }
