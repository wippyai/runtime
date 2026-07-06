-- SPDX-License-Identifier: MPL-2.0

-- Demo command: submits the two-wave PoC deploy (postgres, then an app
-- container whose DATABASE_URL is a $ref onto the postgres outputs) and
-- polls the deploy to a terminal status.
local funcs = require("funcs")
local json = require("json")
local time = require("time")
local io = require("io")

local POLL_ATTEMPTS = 600

local function main()
	local deploy_id = "demo-" .. tostring(time.now():unix_nano())

	local input = {
		deploy_id = deploy_id,
		scope = "poc",
		specs = {
			{
				resource_id = "poc/postgres",
				type = "container",
				provider_type = "docker",
				desired = {
					name = "poc-postgres",
					image = "postgres:18-alpine",
					env = { POSTGRES_PASSWORD = "pocsecret" },
					ports = { "15432:5432" },
					verify = "pg_isready",
					outputs = {
						dsn = "postgres://postgres:pocsecret@127.0.0.1:15432/postgres",
					},
				},
			},
			{
				resource_id = "poc/app",
				type = "container",
				provider_type = "docker",
				desired = {
					name = "poc-app",
					image = "nginx:alpine",
					env = {
						DATABASE_URL = {
							["$ref"] = { resource_id = "poc/postgres", output_key = "dsn" },
						},
					},
					verify = "running",
				},
			},
		},
	}

	io.print("submitting deploy " .. deploy_id)
	local submitted, submit_err = funcs.call("wippy.cloudgraph:submit_intent", input)
	if submit_err ~= nil then
		error("submit_intent failed: " .. tostring(submit_err))
	end

	io.print("accepted: " .. tostring(submitted))

	for _ = 1, POLL_ATTEMPTS do
		local raw, get_err = funcs.call("wippy.cloudgraph:get_deploy", deploy_id)
		if get_err ~= nil then
			error("get_deploy failed: " .. tostring(get_err))
		end

		local state = json.decode(tostring(raw))
		local deploy = state.deploy or {}
		local status = tostring(deploy.status)

		local line = "status=" .. status
		local operations = state.operations or {}
		for i = 1, #operations do
			local op = operations[i]
			if type(op) == "table" then
				line = line .. "  " .. tostring(op.resource_id) .. "=" .. tostring(op.status)
			end
		end

		io.print(line)

		if status == "succeeded" then
			io.print("deploy succeeded, finalized_seq=" .. tostring(deploy.finalized_seq))
			return { ok = true, deploy_id = deploy_id }
		end

		if status == "failed" or status == "partial" then
			error("deploy finished " .. status .. ": " .. tostring(deploy.error))
		end

		time.sleep(1 * time.SECOND)
	end

	error("timed out waiting for deploy " .. deploy_id)
end

return { main = main }
