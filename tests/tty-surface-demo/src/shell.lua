-- SPDX-License-Identifier: MPL-2.0

local channel = require("channel")
local process = require("process")
local time = require("time")
local tty = require("tty")

local function main()
    assert(tty.start())
    local events = assert(tty.events())
    local lifecycle = assert(process.events())
    local output = assert(tty.surface({
        alternate_screen = true,
        hide_cursor = true,
        synchronized_output = true,
    }))

    local width, height = tty.screen_size()
    width, height = math.floor(width or 80), math.floor(height or 24)
    local viewport = assert(tty.viewport({width = width, height = math.max(1, height - 1)}))
    local updates = assert(viewport:updates())
    local grant = assert(viewport:grant())
    local child = assert(process.with_options({terminal = grant})
        :spawn_monitored("tty_proof:child", "tty_proof:workers", "/bin/bash --noprofile --norc"))

    local revision = -1
    local snapshot: any = {rows = {}}
    local ready = false
    local closing = false
    local deadline

    local function draw()
        local rows = {tty.text.truncate(
            " Wippy TTY proof — real Bash in a process-owned viewport (Ctrl+Q exits) ",
            width)}
        for index = 1, height - 1 do
            rows[index + 1] = snapshot.rows[index] or ""
        end
        local cursor
        if snapshot.cursor then
            cursor = {
                x = snapshot.cursor.x,
                y = snapshot.cursor.y + 1,
                visible = snapshot.cursor.visible,
            }
        end
        assert(output:present(rows, {cursor = cursor}))
    end

    draw()
    while true do
        local cases = {
            events:case_receive(),
            lifecycle:case_receive(),
            updates:case_receive(),
        }
        if deadline then cases[#cases + 1] = deadline:case_receive() end
        local selected = channel.select(cases)
        if not selected.ok then break end

        if selected.channel == updates then
            local next = viewport:snapshot(revision)
            if next then
                snapshot, revision = next, next.revision
                ready = true
                draw()
            end
        elseif selected.channel == lifecycle then
            local event = selected.value
            if event.kind == process.event.EXIT and event.from == child then break end
        elseif deadline and selected.channel == deadline then
            assert(process.terminate(child))
            deadline = nil
        else
            local event = selected.value
            if event.type == "resize" then
                width = math.floor(tonumber(event.width) or width)
                height = math.floor(tonumber(event.height) or height)
                assert(viewport:resize(width, math.max(1, height - 1)))
                output:invalidate()
                draw()
            elseif event.type == "key" and event.ctrl and event.key == "q" then
                if not closing then
                    closing = true
                    if ready then
                        assert(viewport:send({type = "close"}))
                    else
                        assert(process.terminate(child))
                    end
                    deadline = time.after("3s")
                end
            elseif not closing and ready and event.type ~= "start" then
                assert(viewport:send(event))
            end
        end
    end

    assert(viewport:close())
    assert(output:close())
    assert(tty.stop())
end

return {main = main}
