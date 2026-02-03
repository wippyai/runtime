local function handler()
	local http = require("http")
	local security = require("security")
	local json = require("json")

	local req = http.request()
	local res = http.response()

	local body = req:body()
	if not body or body == "" then
		res:set_status(http.STATUS.BAD_REQUEST)
		res:write_json({ error = "request body required" })
		return
	end

	local data, err = json.decode(body)
	if err then
		res:set_status(http.STATUS.BAD_REQUEST)
		res:write_json({ error = "invalid JSON: " .. tostring(err) })
		return
	end

	local username = string(data.username)
	if not username or username == "" then
		res:set_status(http.STATUS.BAD_REQUEST)
		res:write_json({ error = "username required" })
		return
	end

	local store, err = security.token_store("app.test.http_auth:tokens")
	if err then
		res:set_status(http.STATUS.INTERNAL_SERVER_ERROR)
		res:write_json({ error = "failed to acquire token store: " .. tostring(err) })
		return
	end

	local actor = security.new_actor(username, {
		type = "user",
		username = username
	})

	local allow_policy, err = security.policy("app.test.http_auth:allow_access")
	if err then
		res:set_status(http.STATUS.INTERNAL_SERVER_ERROR)
		res:write_json({ error = "failed to get policy: " .. tostring(err) })
		return
	end

	local scope = security.new_scope({ allow_policy })

	local token, err = store:create(actor, scope, {
		expiration = "1h",
		meta = { login_time = os.time() }
	})

	if err then
		res:set_status(http.STATUS.INTERNAL_SERVER_ERROR)
		res:write_json({ error = "failed to create token: " .. tostring(err) })
		return
	end

	res:set_status(http.STATUS.OK)
	res:set_header("X-Auth-Token", token)
	res:write_json({
		token = token,
		actor_id = username,
		expires_in = "1h"
	})
end

return { handler = handler }
