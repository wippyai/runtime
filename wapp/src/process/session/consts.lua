-- Session and controller status constants
local STATUS = {
    IDLE = "idle",
    RUNNING = "running",
    ERROR = "error",
    FAILED = "failed"
}

-- Task types for the controller
local TASK_TYPE = {
    MESSAGE = "message",             -- Process a user message
    AGENT_CHANGE = "agent_change",   -- Change the current agent
    MODEL_CHANGE = "model_change",   -- Change the current model
    TOOL_CONTINUE = "tool_continue", -- Continue after tool execution
    DELEGATION = "delegation"        -- Continue after delegation
}

-- Control result types from tools
local CONTROL_TYPE = {
    MODEL_CHANGE = "model_change",
    DELEGATION = "delegation"
}

-- Topics for actor system
local TOPICS = {
    MESSAGE = "message",
    COMMAND = "command",
    CONTINUE = "continue",
    CONTEXT = "context",
    TITLE = "title",
    PUBLIC_META = "public_meta"
}

-- Commands for external API
local COMMANDS = {
    STOP = "stop",
    MODEL = "model",
    AGENT = "agent"
}

-- Message type constants
local MSG_TYPE = {
    SYSTEM = "system",
    USER = "user",
    ASSISTANT = "assistant",
    DEVELOPER = "developer",
    FUNCTION = "function",
    DELEGATION = "delegation",
    AGENT_CHANGE = "agent_change",
    MODEL_CHANGE = "model_change"
}

-- Function status constants
local FUNC_STATUS = {
    PENDING = "pending",
    SUCCESS = "success",
    ERROR = "error"
}

-- Error codes (used mainly for client communication)
local ERROR_CODE = {
    FAILED = "FAILED",
    ERROR = "ERROR",
    BUSY = "BUSY",
    AGENT_ERROR = "AGENT_ERROR",
    DB_ERROR = "DB_ERROR",
    PROMPT_ERROR = "PROMPT_ERROR",
    DELEGATION_ERROR = "DELEGATION_ERROR",
    RESPONSE_ERROR = "RESPONSE_ERROR",
}

-- Common error messages
local ERR = {
    -- Session errors
    MISSING_ARGS = "User ID and session ID are required",
    MISSING_TOKEN = "Start token is required for new session",
    FAILED_STATE = "Session is in a failed state and cannot process messages",
    FAILED_COMMANDS = "Session is in a failed state and cannot process commands",
    INIT_FAILED = "Session initialization failed",
    EXEC_AGENT = "Error executing agent",
    BUSY = "Session is already processing a message",
    UNSUPPORTED_COMMAND = "Unsupported command",

    -- Controller errors
    EMPTY_MESSAGE = "Message text cannot be empty",
    NO_AGENT = "No agent configured for this session",
    AGENT_LOAD_FAILED = "Failed to load agent",
    MESSAGE_ID_FAILED = "Failed to generate message ID",
    RESPONSE_ID_FAILED = "Failed to generate response ID",
    AGENT_NAME_REQUIRED = "Agent name is required",
    MODEL_NAME_REQUIRED = "Model name is required",
    DELEGATION_FAILED = "Failed to delegate to agent",
    QUEUE_EMPTY = "No payloads in queue to process",

    -- State errors
    STORE_MESSAGE_FAILED = "Failed to store message",
    FUNCTION_NAME_REQUIRED = "Function name is required",
    FUNCTION_RESULT_REQUIRED = "Function result is required",
    FUNCTION_CALL_ID_REQUIRED = "Function call ID is required"
}

return {
    STATUS = STATUS,
    TASK_TYPE = TASK_TYPE,
    COMMANDS = COMMANDS,
    TOPICS = TOPICS,
    MSG_TYPE = MSG_TYPE,
    FUNC_STATUS = FUNC_STATUS,
    ERROR_CODE = ERROR_CODE,
    ERR = ERR
}
