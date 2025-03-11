```lua
{
    -- Core parameters
    model = "claude-3-7-sonnet-20250219",  
    messages = messages,                   
    
    -- Thinking configuration 
    thinking_enabled = true,
    thinking_budget = 2048,
    
    -- Tool configuration
    tool_ids = {"system:weather", "tools:calculator"},
    tool_schemas = {
        -- manually defined tools in a format of tool_resolver
    },                     
    tool_call_behavior = "auto",           
    
    -- Streaming configuration
    stream = false,
    reply_to = "process-id",               
    topic = "llm_response",                
    
    -- Generation parameters (optional)
    temperature = 0.7,
    top_p = 0.9,
    top_k = 40,
    max_tokens = 1024,
    stop_sequences = {"\n\nHuman:"}
}
```