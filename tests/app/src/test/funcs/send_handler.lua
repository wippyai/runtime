local function lookup(name)
	local target, err = process.registry.lookup(name)
	if err then
		return nil, err
	end
	return target
end

local function send_statement(name, value)
	local target, err = lookup(name)
	if err then
		return nil, err
	end
	local sent, send_err = process.send(target, "mcp.command", { value = value })
	return sent, send_err
end

local function send_tail(name, value)
	local target, err = lookup(name)
	if err then
		return nil, err
	end
	return process.send(target, "mcp.command", { value = value })
end

local function send_pressure(name, value)
	local ok, result = pcall(function()
		local title = "title"
		local class = "class"
		local user_id = "user"
		local actor_id = "actor"
		local actor_scope = "scope"
		local schedule_type = "interval"
		local schedule_expression = "1h"
		local timeout_seconds = 300
		local max_retries = 3
		local enabled = true
		local task_args = {
			title = title,
			class = class,
			user_id = user_id,
			actor_id = actor_id,
			actor_scope = actor_scope,
			schedule_type = schedule_type,
			schedule_expression = schedule_expression,
			timeout_seconds = timeout_seconds,
			max_retries = max_retries,
			enabled = enabled,
		}
		local target, lookup_err = lookup(name)
		if lookup_err then
			error(lookup_err)
		end
		local sent, send_err = process.send(target, "mcp.command", {
			value = value,
			task_args = task_args,
		})
		if send_err then
			error(send_err)
		end
		return sent
	end)
	if not ok then
		return nil, result
	end
	return result
end

return {
	send_statement = send_statement,
	send_tail = send_tail,
	send_pressure = send_pressure,
}
