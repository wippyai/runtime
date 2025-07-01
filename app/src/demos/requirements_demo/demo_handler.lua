-- Demo handler function showcasing requirements and dependencies
-- This function demonstrates how the new ns.requirement and ns.dependency system works

local http = require("http")
local env = require("env")
local registry = require("registry")

-- Handler function that gets called when the endpoint is accessed
local function handler()
    -- Set up response
    local res = http.response()
    if not res then
        return nil, "Failed to create HTTP response"
    end

    -- Set content type and status
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)

    local response = {
        message = "Requirements and Dependencies Demo",
        timestamp = os.date("%Y-%m-%d %H:%M:%S"),
        requirements = {},
        dependencies = {},
        parameter_injection = {}
    }
    
    -- Demonstrate environment variable access (from DATABASE_NAME)
    local db_name = env.get("db_config")
    if db_name then
        response.environment = {
            database_name = db_name
        }
    end
    
    -- Dynamically resolve requirements from registry
    local requirements_entries = registry.find({
        [".kind"] = "ns.requirement"
    })
    
    response.requirements = {}
    for _, entry in ipairs(requirements_entries) do
        local req_name = entry.meta and entry.meta.name or entry.id
        local targets = entry.data and entry.data.targets or {}
        
        -- Extract target information for demonstration
        local target_info = {}
        for _, target in ipairs(targets) do
            if target.entry and target.path then
                table.insert(target_info, {
                    target_entry = target.entry,
                    parameter_path = target.path
                })
            end
        end
        
        response.requirements[req_name] = {
            targets = target_info,
            entry_id = entry.id
        }
    end
    
    -- Dynamically resolve dependencies from registry
    local dependency_entries = registry.find({
        [".kind"] = "ns.dependency"
    })
    
    response.dependencies = {}
    for _, entry in ipairs(dependency_entries) do
        local dep_name = entry.meta and entry.meta.name or entry.id
        local component = entry.data and entry.data.component or "unknown"
        local version = entry.data and entry.data.version or "unknown"
        local description = entry.meta and entry.meta.description or ""
        local parameters = entry.data and entry.data.parameters or {}
        
        -- Extract parameter information
        local param_info = {}
        for _, param in ipairs(parameters) do
            if param.name and param.value then
                param_info[param.name] = param.value
            end
        end
        
        response.dependencies[dep_name] = {
            component = component,
            version = version,
            description = description,
            parameters = param_info,
            entry_id = entry.id
        }
    end
    
    -- Demonstrate parameter injection flow
    response.parameter_injection = {
        flow = "Requirements -> Parameter Paths -> Dependencies",
        example = {
            requirement = "NAMESPACE",
            target_entry = "hello_world_dependency",
            parameter_path = "parameters[name=namespace].value",
            injected_value = "app.requirements.demo"
        }
    }
    
    -- Add some metadata about the demo
    response.metadata = {
        description = "This demo shows how requirements and dependencies work with parameter injection",
        features = {
            "ns.requirement entries declare what the application needs",
            "ns.dependency entries specify external components with parameters",
            "Requirements inject values into dependency parameters via target paths",
            "Parameter paths use JSONPath-like syntax for precise targeting",
            "Dependencies can have multiple parameters with default values",
            "The system supports complex parameter injection scenarios"
        },
        yaml_structure = {
            requirements = "Declare needs and specify injection targets",
            dependencies = "Define external components with parameterized configuration",
            targets = "Map requirements to specific parameter paths in dependencies"
        }
    }
    
    -- Write JSON response (use browser dev tools or JSON formatter for pretty printing)
    res:write_json(response)
end

-- Export the function
return {
    handler = handler
} 