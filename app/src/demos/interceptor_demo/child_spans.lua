local otel = require("otel")

local child_spans = {}

-- Process data with child spans
function child_spans.process(ctx, data)
    -- Add attributes and events for data processing
    otel.attribute("data_size", #data)
    otel.event("processing_started", { data_size = #data })
    
    -- Add validation attributes and events
    otel.attribute("validation_type", "format_check")
    otel.event("validation_complete", { status = "success" })
    
    -- Add transformation attributes and events
    otel.attribute("transformation_type", "format_conversion")
    otel.event("transformation_complete", { result = "success" })
    
    -- Mark processing as complete
    otel.event("processing_completed", { result = "success" })
    otel.status(1, "Processing completed successfully")
    
    return "Processed: " .. data
end

return child_spans 