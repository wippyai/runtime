-- upload.lua
-- File upload handler for processing multipart/form-data uploads

-- Get the required modules
local http = require("http")
local fs = require("fs")

-- Main handler function
local function handler()
  -- Get HTTP request and response objects
  local req, err = http.request()
  local res = http.response()

  if err then
    res:set_status(http.STATUS.INTERNAL_ERROR)
    res:write_json({ error = "Failed to create request context", message = err })
    return
  end

  -- Check HTTP method
  if req:method() ~= http.METHOD.POST then
    res:set_status(http.STATUS.BAD_REQUEST)
    res:write_json({ error = "Method not allowed", message = "Only POST method is supported" })
    return
  end

  -- Check if the request is multipart/form-data
  if not req:is_content_type(http.CONTENT.MULTIPART) then
    res:set_status(http.STATUS.BAD_REQUEST)
    res:write_json({ error = "Invalid content type", message = "Expected multipart/form-data" })
    return
  end

  -- Parse multipart form with a 50MB limit
  local form, err = req:parse_multipart(50 * 1024 * 1024)
  if err then
    res:set_status(http.STATUS.BAD_REQUEST)
    res:write_json({ error = "Failed to parse form", message = err })
    return
  end

  -- Check if we have any files
  if not form.files or not next(form.files) then
    res:set_status(http.STATUS.BAD_REQUEST)
    res:write_json({ error = "No files found", message = "No files were uploaded" })
    return
  end

  -- Get the local filesystem
  local fsObj = fs.get("system:root")
  if not fsObj then
    res:set_status(http.STATUS.INTERNAL_ERROR)
    res:write_json({ error = "Filesystem error", message = "Could not access the filesystem" })
    return
  end

  -- Create the uploads directory if it doesn't exist
  local uploadDir = "/uploads"
  if not fsObj:exists(uploadDir) then
    local success, err = pcall(function() fsObj:mkdir(uploadDir) end)
    if not success then
      res:set_status(http.STATUS.INTERNAL_ERROR)
      res:write_json({ error = "Directory creation failed", message = err })
      return
    end
  end

  -- Process each uploaded file
  local results = {}
  local fieldName, fileArray = next(form.files)

  while fieldName do
    for i, file in ipairs(fileArray) do
      local filename = file:name()
      local size = file:size()
      local targetPath = uploadDir .. "/" .. filename

      -- Check if the file already exists
      if fsObj:exists(targetPath) then
        -- Add timestamp to filename to avoid overwriting
        local timestamp = os.time()
        local nameParts = {}
        for part in string.gmatch(filename, "[^%.]+") do
          table.insert(nameParts, part)
        end

        if #nameParts > 1 then
          -- Has extension
          local ext = nameParts[#nameParts]
          local basename = string.sub(filename, 1, #filename - #ext - 1)
          filename = basename .. "_" .. timestamp .. "." .. ext
        else
          -- No extension
          filename = filename .. "_" .. timestamp
        end

        targetPath = uploadDir .. "/" .. filename
      end

      -- Create a stream from the uploaded file
      local stream, err = file:stream()
      if err then
        table.insert(results, {
          field = fieldName,
          filename = filename,
          success = false,
          error = "Failed to create stream: " .. err
        })
      else
        -- Write the stream to the target file
        local success, err = fsObj:writefile(targetPath, stream, "w")
        if success then
          table.insert(results, {
            field = fieldName,
            filename = filename,
            path = targetPath,
            size = size,
            success = true
          })
        else
          table.insert(results, {
            field = fieldName,
            filename = filename,
            success = false,
            error = "Failed to save file: " .. tostring(err)
          })
        end
      end
    end

    -- Move to the next field
    fieldName, fileArray = next(form.files, fieldName)
  end

  -- Send the results back to the client
  local allSuccess = true
  for _, result in ipairs(results) do
    if not result.success then
      allSuccess = false
      break
    end
  end

  if allSuccess then
    res:set_status(http.STATUS.OK)
  else
    res:set_status(http.STATUS.PARTIAL_CONTENT)
  end

  res:write_json({
    message = allSuccess and "All files uploaded successfully" or "Some files failed to upload",
    files = results
  })
end

return handler
