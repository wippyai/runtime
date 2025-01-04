local treesitter = require("treesitter")

-- Test code with multiple nested languages using backticks for raw strings
local code = [[
func main() {
	// Example of embedded JavaScript
	js := `function processUser(id) {
    return fetchUserData(id)
        .then(data => processData(data));
}`

	// Example of HTML template
	template := `<div class="user-profile">
    <script type="text/javascript">
        const userId = {{.UserID}};
        processUser(userId);
    </script>
    <style>
        .user-profile { padding: 20px; }
    </style>
</div>`
}
]]

-- First parse the Go code
local parser = treesitter.parser()
assert(parser:set_language("go"), "Failed to set Go language")

local tree = parser:parse(code)
local root = tree:root_node()

-- Create query to find string literals with better context
local raw_string_query = treesitter.query("go", [[
    (short_var_declaration
        left: (expression_list (identifier) @var_name)
        right: (expression_list (raw_string_literal) @raw_string))
]])

-- Function to clean content
local function clean_content(raw_str)
    -- Remove backticks and normalize indentation
    local content = raw_str:gsub("^`", ""):gsub("`$", "")
    local lines = {}
    local min_indent = math.huge

    for line in content:gmatch("[^\n]+") do
        if line:match("%S") then
            local indent = line:match("^%s*"):len()
            min_indent = math.min(min_indent, indent)
        end
        table.insert(lines, line)
    end

    for i, line in ipairs(lines) do
        if line:match("%S") then
            lines[i] = line:sub(min_indent + 1)
        end
    end

    return table.concat(lines, "\n")
end

local matches = raw_string_query:matches(root, code)
assert(#matches == 2, "Should find exactly 2 raw string declarations")

local found_js = false
local found_html = false

for _, match in ipairs(matches) do
    local var_name = match.captures[1].node:text(code)
    local raw_str = match.captures[2].node:text(code)
    local content = clean_content(raw_str)

    if var_name == "js" then
        -- Handle JavaScript
        local js_parser = treesitter.parser()
        assert(js_parser:set_language("javascript"), "Failed to set JavaScript language")

        local js_tree = js_parser:parse(content)
        local js_root = js_tree:root_node()

        local js_query = treesitter.query("javascript", [[
            (function_declaration
                name: (identifier) @func_name
                parameters: (formal_parameters (identifier) @param)
                body: (statement_block
                    (return_statement
                        (call_expression
                            function: (member_expression
                                object: (call_expression
                                    function: (identifier) @call_func
                                    arguments: (arguments (identifier)))
                                property: (property_identifier) @method_name)
                            arguments: (arguments
                                (arrow_function
                                    parameter: (identifier) @callback_param
                                    body: (call_expression)))))))
        ]])

        local js_matches = js_query:matches(js_root, content)
        assert(#js_matches == 1, "Should find exactly one JavaScript function")

        -- Verify JavaScript structure
        local js_match = js_matches[1]
        for _, capture in ipairs(js_match.captures) do
            local text = capture.node:text(content)
            if capture.name == "func_name" then
                assert(text == "processUser", "Function should be named processUser")
            elseif capture.name == "param" then
                assert(text == "id", "Parameter should be named id")
            elseif capture.name == "call_func" then
                assert(text == "fetchUserData", "Should call fetchUserData")
            elseif capture.name == "method_name" then
                assert(text == "then", "Should use then method")
            elseif capture.name == "callback_param" then
                assert(text == "data", "Callback parameter should be data")
            end
        end

        found_js = true

    elseif var_name == "template" then
        -- Handle HTML
        local html_parser = treesitter.parser()
        assert(html_parser:set_language("html"), "Failed to set HTML language")

        local html_tree = html_parser:parse(content)
        local html_root = html_tree:root_node()

        local html_query = treesitter.query("html", [[
            (document
                (element
                    (start_tag
                        (tag_name) @root_tag) @div_tag
                    (script_element) @script
                    (style_element) @style
                    (end_tag)))
        ]])

        local html_matches = html_query:matches(html_root, content)
        assert(#html_matches == 1, "Should find one HTML template")

        -- Verify HTML structure
        local html_match = html_matches[1]
        for _, capture in ipairs(html_match.captures) do
            local text = capture.node:text(content)
            if capture.name == "root_tag" then
                assert(text == "div", "Root element should be div")
            elseif capture.name == "script" then
                assert(text:match("processUser"), "Script should contain processUser")
            elseif capture.name == "style" then
                assert(text:match("padding: 20px"), "Style should define padding")
            end
        end

        found_html = true
    end
end

assert(found_js, "JavaScript content should be found and validated")
assert(found_html, "HTML content should be found and validated")