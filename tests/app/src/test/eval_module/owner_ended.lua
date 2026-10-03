-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert")
local time = require("time")
local funcs = require("funcs")

local function main()
 local events = process.events()
 local inbox = process.inbox()
 local future, err = funcs.async("app.test.eval_module:owner_wait", process.pid())
 assert.is_nil(err, "owner function starts")
 local timeout = time.after("5s")
 local ready = channel.select { inbox:case_receive(), timeout:case_receive() }
 assert.eq(ready.channel, inbox, "owner reports its eval")
 local pids = ready.value:payload():data()
 assert.ok(process.monitor(pids.eval), "eval is monitored before owner ends")
 assert.ok(process.send(pids.owner, "end"), "owner may return")
 while true do
  local selected = channel.select { events:case_receive(), timeout:case_receive() }
  assert.eq(selected.channel, events, "eval exit arrives")
  local event = selected.value
  if event.kind == process.event.EXIT and event.from == pids.eval then
   assert.not_nil(event.result.error, "owner ending fails the eval")
   assert.eq(event.result.error:kind(), "InvalidState", "owner ending has a typed error")
   assert.eq(event.result.error:message(), "owning execution has ended", "eval exit identifies owner ending")
   assert.eq(event.result.error:retryable(), false, "owner ending is not retryable")
   break
  end
 end
 future:response():receive()
 return true
end

return { main = main }
