-- SPDX-License-Identifier: MPL-2.0

local function main(observer)
 local inbox = process.inbox()
 local child, err = eval.spawn("return { main = function() while true do end end }", { monitor_only = true })
 if err then error(err) end
 process.send(observer, "owned", { owner = process.pid(), eval = child })
 inbox:receive()
 return true
end

return { main = main }
