local http = require("http")
local system = require("system")

local function format_bytes(bytes)
    if bytes < 1024 then
        return string.format("%d B", bytes)
    elseif bytes < 1024 * 1024 then
        return string.format("%.2f KB", bytes / 1024)
    elseif bytes < 1024 * 1024 * 1024 then
        return string.format("%.2f MB", bytes / (1024 * 1024))
    else
        return string.format("%.2f GB", bytes / (1024 * 1024 * 1024))
    end
end

local function handler()
    local res = http.response()
    if not res then
        return nil, "Failed to get HTTP response"
    end

    local mem, mem_err = system.memory.stats()
    if mem_err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({error = mem_err})
        return
    end

    local goroutines = system.runtime.goroutines()

    local data = {
        goroutines = goroutines,
        memory = {
            alloc = mem.alloc,
            alloc_human = format_bytes(mem.alloc),
            total_alloc = mem.total_alloc,
            total_alloc_human = format_bytes(mem.total_alloc),
            sys = mem.sys,
            sys_human = format_bytes(mem.sys),
            heap_alloc = mem.heap_alloc,
            heap_alloc_human = format_bytes(mem.heap_alloc),
            heap_sys = mem.heap_sys,
            heap_sys_human = format_bytes(mem.heap_sys),
            heap_idle = mem.heap_idle,
            heap_idle_human = format_bytes(mem.heap_idle),
            heap_in_use = mem.heap_in_use,
            heap_in_use_human = format_bytes(mem.heap_in_use),
            heap_released = mem.heap_released,
            heap_released_human = format_bytes(mem.heap_released),
            heap_objects = mem.heap_objects,
            stack_in_use = mem.stack_in_use,
            stack_in_use_human = format_bytes(mem.stack_in_use),
            num_gc = mem.num_gc,
        }
    }

    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json(data)
end

return {
    handler = handler
}
