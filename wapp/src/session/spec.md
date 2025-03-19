# Revised Session Message Data Formats

Here are the refined message data formats for the session system, aligned with the prompt library and your requirements:

## 1. User Messages

```json
{
  "message_id": "msg_uuid",
  "session_id": "session_uuid",
  "type": "user",
  "date": 1710845234,
  "data": "What is the correlation between sales and marketing spend?",
  "metadata": {
    "source": "web",
    "files": [
      "file_uuid1",
      "file_uuid2"
    ]
  }
}
```

## 2. Assistant Messages

```json
{
  "message_id": "msg_uuid",
  "session_id": "session_uuid",
  "type": "assistant",
  "date": 1710845240,
  "data": "Based on the data, there is a strong positive correlation (r=0.78) between marketing spend and sales revenue...",
  "metadata": {
    "agent_id": "data-analyst",
    "model": "gpt-4o",
    "tokens": {
      "prompt": 1245,
      "completion": 876,
      "thinking": 320,
      "total": 2441
    },
    "thinking": "Let me analyze this by looking at the statistical relationship between..."
  }
}
```

## 3. Tool Call Messages

```json
{
  "message_id": "msg_uuid",
  "session_id": "session_uuid",
  "type": "tool_call",
  "date": 1710845252,
  "data": {
    "tool_name": "Sales Data Analyzer",
    "description": "Analyzing Q1 sales data for correlation with marketing spend"
  },
  "metadata": {
    "agent_id": "data-analyst",
    "tool_id": "system:sales_analyzer",
    "call_id": "tool_call_uuid",
    "arguments": {
      "dataset": "q1_sales.csv",
      "metrics": [
        "revenue",
        "marketing_spend"
      ],
      "analysis_type": "correlation"
    }
  }
}
```

## 4. Tool Result Messages

```json
{
  "message_id": "msg_uuid",
  "session_id": "session_uuid",
  "type": "tool_result",
  "date": 1710845258,
  "data": {
    "tool_name": "Sales Data Analyzer",
    "result_summary": "Found correlation coefficient r=0.78 between marketing spend and sales",
    "parent_call_id": "tool_call_uuid"
  },
  "metadata": {
    "tool_id": "system:sales_analyzer",
    "result": {
      "correlation": 0.78,
      "p_value": 0.003,
      "sample_size": 92,
      "confidence_interval": [
        0.65,
        0.86
      ],
      "chart_data": {
        /* raw data points */
      }
    }
  }
}
```

## 5. Agent Change Messages

```json
{
  "message_id": "msg_uuid",
  "session_id": "session_uuid",
  "type": "agent_change",
  "date": 1710845270,
  "data": {
    "from_agent": "Navigator",
    "to_agent": "Data Analyst",
    "message": "Switching to Data Analyst for specialized sales correlation analysis"
  },
  "metadata": {
    "from_agent_id": "navigator-manager",
    "to_agent_id": "data-analyst",
    "delegation_message": "Please analyze the correlation between marketing spend and sales performance",
    "message_id": "parent_msg_uuid"
  }
}
```

## 6. Model Change Messages

```json
{
  "message_id": "msg_uuid",
  "session_id": "session_uuid",
  "type": "model_change",
  "date": 1710845285,
  "data": {
    "from_model": "GPT-4o",
    "to_model": "Claude 3.7 Sonnet",
    "message": "Switching to Claude for analysis"
  },
  "metadata": {
    "from_model_id": "gpt-4o",
    "to_model_id": "claude-3-7-sonnet",
    "agent_id": "data-analyst",
    "message_id": "parent_msg_uuid"
  }
}
```

## 7. Checkpoint Messages

```json
{
  "message_id": "msg_uuid",
  "session_id": "session_uuid",
  "type": "checkpoint",
  "date": 1710845300,
  "data": {
    "checkpoint_type": "auto",
    "sequence": 3,
    "message": "Conversation checkpoint created"
  },
  "metadata": {
    "checkpoint_id": "checkpoint_uuid",
    "message_range": {
      "first_msg_id": "first_msg_uuid",
      "last_msg_id": "last_msg_uuid",
      "count": 12
    },
    "summary": null,
    // Will be populated later
    "state": {
      "agent_id": "data-analyst",
      "model": "claude-3-7-sonnet",
      "tokens_used": 4532
    }
  }
}
```

## 8. System Messages

```json
{
  "message_id": "msg_uuid",
  "session_id": "session_uuid",
  "type": "system",
  "date": 1710845320,
  "data": {
    "message": "Session recovered from previous state",
    "level": "info"
  },
  "metadata": {
    "code": "session.recovered",
    "details": {
      "last_active": 1710842320,
      "messages_loaded": 47
    }
  }
}
```

## Key Adjustments

1. **Removed client_id**: Eliminated as user_id is available at session level
2. **Tool Call Format**: Simplified by removing status field - tool calls complete by result
3. **Tool Result Format**: Added parent_call_id to the public data for reference
4. **Agent Change**: Changed "reason" to "message" and removed delegation_tool
5. **Model Change**: Changed "reason" to "message" to reflect the communication intent
6. **All Messages**: Ensured compatibility with the prompt library's function_call and function_result methods

This revised format aligns with the prompt library's approach to handling function calls and results, while maintaining
the clean separation between user-facing content (`data` field) and system information (`metadata` field).



------------------------------------------

# Refined Session Process Communication Protocol

## Session Initialization Data
```lua
{
  session_id = "session_uuid",          -- Unique session identifier 
  user_id = "user_uuid",                -- User identifier (required)
  parent_pid = "parent_process_pid",    -- Parent process for control messages
  conn_pid = "connection_process_pid",  -- Connection process for updates/responses
  primary_context_id = "context_uuid",  -- Initial context (optional)
  start_model = "gpt-4o",               -- Initial model (optional)
  start_agent = "data-analyst",         -- Initial agent (optional)
  kind = "default"                      -- Session kind (chat, rag, etc.)
}
```
*Note: Recovery is automatic based on session_id - if session exists, state is loaded from DB*

## Incoming Messages

### 1. User Message (Topic: `session.message`)
```lua
{
  data = "User message text",           -- User message content
  metadata = {                          -- Optional metadata
    source = "web",
    files = ["file_uuid1", "file_uuid2"]
  }
}
```
*Session process already knows session_id and user_id*

### 2. Commands (Topic: `session.command`)
```lua
{
  command = "change_model",             -- Command type
  model = "claude-3-7-sonnet"           -- Command-specific data
}
```
*Other commands: `change_agent`, `cancel`, `clear_history`, `update_conn_pid`*

## Outgoing Messages and Updates (All to conn_pid)

### 1. Message Stream (Topic: `session:{session_id}:{message_id}`)
```lua
{
  type = "content",                     -- "content", "thinking", "done", "error"
  content = "Content chunk text",       -- For content chunks
  thinking = "Thinking process text"    -- For thinking chunks
}
```

### 2. Message ID Response (Topic: `session.message.ack`)
```lua
{
  message_id = "msg_uuid",              -- Generated UUID for the message
  status = "created"                    -- Or "rejected" if processing another message
}
```

### 3. Session Status Updates (Topic: `session:{session_id}:status`)
```lua
{
  type = "agent_change",                -- Type of update
  from = "Navigator",
  to = "Data Analyst",
  timestamp = 1710845270
}
```
*Other update types: `model_change`, `processing_started`, `processing_complete`, `error`*

## Session Process Behavior Notes

1. Session automatically recovers if initialized with existing session_id
2. User messages are written to DB first, then ID is returned on session.message.ack
3. If session is actively processing, new messages are rejected with status "rejected"
4. If recovery finds a pending last message, it's deleted
5. All status updates go to the conn_pid (which can be updated via command)
6. Session tracks and records token usage for all LLM calls
7. All tool calls are handled internally within the session process
8. Session state changes (agent, model) are recorded in DB

Is this better aligned with your requirements? Shall I proceed with implementing the session manager class?