local M = {}

function M.analyze(filepath)
    local fs = require("fs")
    local myfs = fs.get("system:root")

    if not myfs then
        return nil, "Failed to get filesystem"
    end

    local content = myfs:readfile(filepath)
    if not content then
        return nil, "Failed to read file"
    end

    -- Analyze markdown content
    local analysis = {
        headers = {},
        links = {},
        images = {},
        codeBlocks = 0,
        totalLines = 0
    }

    -- Parse the content line by line
    for line in content:gmatch("[^\r\n]+") do
        analysis.totalLines = analysis.totalLines + 1

        -- Headers (both # and === style)
        local header = line:match("^(#+)%s+(.+)")
        if header then
            table.insert(analysis.headers, line:match("^#+%s+(.+)"))
        elseif line:match("^=+%s*$") and analysis.totalLines > 1 then
            -- Previous line was a header
            table.insert(analysis.headers, "# " .. prev_line)
        elseif line:match("^%-+%s*$") and analysis.totalLines > 1 then
            -- Previous line was a header
            table.insert(analysis.headers, "## " .. prev_line)
        end

        -- Links [text](url)
        for link in line:gmatch("%[([^%]]+)%]%(([^%)]+)%)") do
            table.insert(analysis.links, link)
        end

        -- Images ![alt](url)
        for image in line:gmatch("!%[([^%]]+)%]%(([^%)]+)%)") do
            table.insert(analysis.images, image)
        end

        -- Code blocks
        if line:match("^```") then
            analysis.codeBlocks = analysis.codeBlocks + 1
        end

        prev_line = line
    end

    -- Count items
    local header_count = 0
    local link_count = 0
    local image_count = 0

    for _ in pairs(analysis.headers) do header_count = header_count + 1 end
    for _ in pairs(analysis.links) do link_count = link_count + 1 end
    for _ in pairs(analysis.images) do image_count = image_count + 1 end

    -- Generate text report
    local report = string.format([[
Markdown Analysis Report
----------------------
Structure:
  Headers: %d
  Links: %d
  Images: %d
  Code Blocks: %d
  Total Lines: %d

Headers Found:
%s

Links Found:
%s

Images Found:
%s
]],
        header_count,
        link_count,
        image_count,
        analysis.codeBlocks,
        analysis.totalLines,
        table.concat(analysis.headers, "\n"),
        table.concat(analysis.links, "\n"),
        table.concat(analysis.images, "\n")
    )

    return {
        text = report
    }
end

return M