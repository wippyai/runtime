-- Probe workflow API behavior for unsupported link operations in workflow context.
local function main()
	local self_pid, pid_err = process.pid()
	if pid_err ~= nil then
		return {
			pid_ok = false,
			pid_error = tostring(pid_err),
		}
	end

	local link_ok, link_err = process.link(self_pid)
	return {
		pid_ok = true,
		pid = self_pid,
		link_ok = link_ok,
		link_error = link_err and tostring(link_err) or nil,
	}
end

return main

