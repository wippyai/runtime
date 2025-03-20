local session_updater = {}
session_updater.__index = session_updater

function session_updater.new(session_id, conn_pid, parent_pid)
    local self = setmetatable({}, session_updater)
    self.session_id = session_id
    self.conn_pid = conn_pid
    self.parent_pid = parent_pid
    return self
end

-- Topic generation methods --

-- Get session-level topic
function session_updater:get_session_topic()
    return "session:" .. self.session_id
end

-- Get message-level topic
function session_updater:get_message_topic(message_id)
    return "session:" .. self.session_id .. ":message:" .. message_id
end

-- SESSION-LEVEL UPDATES --

-- Update session state (status, agent, model, etc.)
function session_updater:update_session(changes)
    changes["session_id"] = self.session_id
    self:_send_session_update("update", changes)
end

-- Report session-level error
function session_updater:session_error(code, message)
    self:_send_session_update("error", {
        code = code,
        message = message
    })
end

-- MESSAGE-LEVEL UPDATES --

-- Confirm message reception
function session_updater:message_received(message_id, text)
    self:_send_message_update(message_id, "received", {
        message_id = message_id,
        text = text,
        timestamp = os.time()
    })
end

-- Report message-level error
function session_updater:message_error(message_id, code, message)
    self:_send_message_update(message_id, "error", {
        message_id = message_id,
        code = code,
        message = message
    })
end

-- PRIVATE METHODS --

-- Send session-level update
function session_updater:_send_session_update(type, payload)
    local topic = self:get_session_topic()
    local message = { type = type }

    -- Merge payload fields into message
    for k, v in pairs(payload or {}) do
        message[k] = v
    end

    self:_send_message(topic, message)
end

-- Send message-level update
function session_updater:_send_message_update(message_id, type, payload)
    local topic = self:get_message_topic(message_id)
    local message = { type = type }

    -- Merge payload fields into message
    for k, v in pairs(payload or {}) do
        message[k] = v
    end

    self:_send_message(topic, message)
end

-- Send message to appropriate recipients
function session_updater:_send_message(topic, message)
    -- Send to parent process (which can relay to all connections)
    if self.parent_pid then
        process.send(self.parent_pid, topic, message)
    end
end

return session_updater
