-- SPDX-License-Identifier: MPL-2.0

-- Cloudgraph docker provider actor: provisions one container per operation
-- and reports the closed signal ladder through the durable mailbox
-- (docs/design/cloud-graph.md I2, I19).
local exec = require("exec")
local time = require("time")

local VERIFY_ATTEMPTS = 60

local function shell_quote(value)
	local s = tostring(value)
	if string.find(s, '"', 1, true) then
		return nil
	end

	return '"' .. s .. '"'
end

local function main(input)
	input = input or {}
	local collector = tostring(input.collector or "")
	if collector == "" then
		return { ok = false, error = "collector pid missing" }
	end

	local seq = 0
	local function next_seq()
		seq = seq + 1
		return seq
	end

	local function send_phase(phase)
		process.send(collector, "cg.signal", { seq = next_seq(), kind = "phase", phase = phase })
	end

	local function send_verify_passed(outputs)
		process.send(collector, "cg.signal", {
			seq = next_seq(),
			kind = "phase",
			phase = "verify_passed",
			payload = { outputs = outputs },
		})
	end

	local function send_checkpoint(container_id)
		process.send(collector, "cg.signal", {
			seq = next_seq(),
			kind = "checkpoint",
			payload = { container_id = container_id },
		})
	end

	local function fail(message)
		local text = tostring(message)
		process.send(collector, "cg.signal", {
			seq = next_seq(),
			kind = "failed",
			payload = { error = text },
		})
		return { ok = false, error = text }
	end

	local desired = input.desired
	if type(desired) ~= "table" then
		return fail("desired document missing")
	end

	local name = tostring(desired.name or "")
	local image = tostring(desired.image or "")
	if name == "" or image == "" then
		return fail("desired.name and desired.image are required")
	end

	local quoted_name = shell_quote(name)
	local quoted_image = shell_quote(image)
	if quoted_name == nil or quoted_image == nil then
		return fail("double quotes are not allowed in name or image")
	end

	local executor, exec_err = exec.get(tostring(input.executor or ""))
	if executor == nil then
		return fail("executor unavailable: " .. tostring(exec_err))
	end

	local function abort(message)
		executor:release()
		return fail(message)
	end

	local function run(cmd)
		local proc, proc_err = executor:exec(cmd)
		if proc == nil then
			return nil, "exec: " .. tostring(proc_err)
		end

		local stdout = proc:stdout_stream()
		local started, start_err = proc:start()
		if not started then
			return nil, "start: " .. tostring(start_err)
		end

		local output = ""
		if stdout ~= nil then
			while true do
				local data = stdout:read()
				if data == nil or data == "" then
					break
				end

				output = output .. tostring(data)
			end

			stdout:close()
		end

		local code, wait_err = proc:wait()
		if wait_err ~= nil then
			return nil, "wait: " .. tostring(wait_err)
		end

		return { code = tonumber(code) or -1, output = output }
	end

	local function is_running()
		local res = run("docker inspect -f {{.State.Running}} " .. quoted_name)
		return res ~= nil and res.code == 0 and string.find(res.output, "true", 1, true) ~= nil
	end

	send_phase("allocate_started")

	local container_id = ""
	local checkpoint = input.checkpoint
	if type(checkpoint) == "table" and checkpoint.container_id ~= nil then
		container_id = tostring(checkpoint.container_id)
	end

	local resumed = container_id ~= "" and is_running()
	if not resumed then
		run("docker rm -f " .. quoted_name)

		local cmd = "docker run -d --name " .. quoted_name
		if type(desired.env) == "table" then
			local keys = {}
			for key in pairs(desired.env) do
				keys[#keys + 1] = tostring(key)
			end

			table.sort(keys)
			for i = 1, #keys do
				local pair = shell_quote(keys[i] .. "=" .. tostring(desired.env[keys[i]]))
				if pair == nil then
					return abort("double quotes are not allowed in env values")
				end

				cmd = cmd .. " -e " .. pair
			end
		end

		if type(desired.ports) == "table" then
			for i = 1, #desired.ports do
				local port = shell_quote(desired.ports[i])
				if port == nil then
					return abort("double quotes are not allowed in port specs")
				end

				cmd = cmd .. " -p " .. port
			end
		end

		cmd = cmd .. " " .. quoted_image
		local res, run_err = run(cmd)
		if res == nil then
			return abort(run_err)
		end

		if res.code ~= 0 then
			return abort("docker run exited " .. tostring(res.code) .. ": " .. res.output)
		end

		container_id = string.gsub(res.output, "%s+", "")
		if container_id == "" then
			return abort("docker run returned no container id")
		end
	end

	send_phase("allocate_committed")
	send_checkpoint(container_id)
	send_phase("configure_committed")

	local verify = tostring(desired.verify or "running")
	local healthy = false
	local last_error = "container never became ready"
	for _ = 1, VERIFY_ATTEMPTS do
		if is_running() then
			if verify == "pg_isready" then
				local probe = run("docker exec " .. quoted_name .. " pg_isready")
				if probe ~= nil and probe.code == 0 then
					healthy = true
					break
				end

				last_error = "pg_isready: " .. tostring(probe ~= nil and probe.output or "probe failed")
			else
				healthy = true
				break
			end
		else
			last_error = "container is not running"
		end

		time.sleep(1 * time.SECOND)
	end

	if not healthy then
		return abort("verify failed: " .. last_error)
	end

	local outputs = {}
	if type(desired.outputs) == "table" then
		for key, value in pairs(desired.outputs) do
			outputs[tostring(key)] = value
		end
	end

	outputs.container_id = container_id
	outputs.name = name

	send_verify_passed(outputs)
	executor:release()
	return { ok = true, container_id = container_id }
end

return { main = main }
