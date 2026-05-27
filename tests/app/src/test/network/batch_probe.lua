-- SPDX-License-Identifier: MPL-2.0

-- Callee for batch_denied. Issues http.request_batch with one plain-clearnet
-- request and one overlay-selecting request. Under a deny-network.select
-- scope the entire batch must reject with the denial error.

local function to_msg(err)
	if err == nil then
		return nil
	end
	if type(err) == "table" then
		return err.message or tostring(err)
	end
	return tostring(err)
end

local function main()
	local http = require("http_client")

	local responses, err = http.request_batch({
		{ "GET", "http://localhost:8085/hello" },
		{
			"GET",
			"http://localhost:8085/hello",
			{
				overlay_network = "app.test.network:fast_socks5",
				timeout = "500ms",
			},
		},
	})

	return {
		responses = responses,
		err = to_msg(err),
	}
end

return { main = main }
