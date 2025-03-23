local sql = require("sql")
local json = require("json")
local uuid = require("uuid")

-- Hardcoded database resource name
local DB_RESOURCE = "app:db"

local file_repo = {}

-- Get a database connection
local function get_db()
    local db, err = sql.get(DB_RESOURCE)
    if err then
        return nil, "Failed to connect to database: " .. err
    end
    return db
end

-- Create a new file record
function file_repo.create(user_id, filename, size, mime_type)
    if not user_id or user_id == "" then
        return nil, "User ID is required"
    end

    if not filename or filename == "" then
        return nil, "Filename is required"
    end

    if not size or size <= 0 then
        return nil, "Valid file size is required"
    end

    if not mime_type or mime_type == "" then
        return nil, "MIME type is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local file_id = uuid.v4()
    local storage_path = user_id .. "/" .. file_id

    -- Insert new file record
    local result, err = db:execute(
        [[INSERT INTO files
          (file_id, user_id, filename, size, mime_type, status, storage_path, created_at, updated_at)
          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)]],
        {
            file_id,
            user_id,
            filename,
            sql.as.int(size),
            mime_type,
            "processing",
            storage_path,
            sql.as.int(os.time()),
            sql.as.int(os.time())
        }
    )

    db:release()

    if err then
        return nil, "Failed to create file record: " .. err
    end

    return {
        file_id = file_id,
        user_id = user_id,
        filename = filename,
        size = size,
        mime_type = mime_type,
        status = "processing",
        storage_path = storage_path
    }
end

-- Save file content
function file_repo.save_content(file_id, content)
    if not file_id or file_id == "" then
        return nil, "File ID is required"
    end

    if not content then
        return nil, "Content is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local content_id = uuid.v7()

    -- Insert file content
    local result, err = db:execute(
        "INSERT INTO file_content (content_id, file_id, content) VALUES (?, ?, ?)",
        { content_id, file_id, content }
    )

    db:release()

    if err then
        return nil, "Failed to save file content: " .. err
    end

    return {
        content_id = content_id,
        file_id = file_id
    }
end

-- Update file status
function file_repo.update_status(file_id, status)
    if not file_id or file_id == "" then
        return nil, "File ID is required"
    end

    if not status or status == "" then
        return nil, "Status is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    -- Update file status
    local result, err = db:execute(
        "UPDATE files SET status = ?, updated_at = ? WHERE file_id = ?",
        { status, sql.as.int(os.time()), file_id }
    )

    db:release()

    if err then
        return nil, "Failed to update file status: " .. err
    end

    if result.rows_affected == 0 then
        return nil, "File not found"
    end

    return {
        file_id = file_id,
        status = status,
        updated = true
    }
end

-- Get a file by ID
function file_repo.get(file_id)
    if not file_id or file_id == "" then
        return nil, "File ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT file_id, user_id, filename, size, mime_type, status, storage_path, created_at, updated_at
        FROM files
        WHERE file_id = ?
    ]]

    local files, err = db:query(query, { file_id })
    db:release()

    if err then
        return nil, "Failed to get file: " .. err
    end

    if #files == 0 then
        return nil, "File not found"
    end

    return files[1]
end

-- Get file content
function file_repo.get_content(file_id)
    if not file_id or file_id == "" then
        return nil, "File ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT content
        FROM file_content
        WHERE file_id = ?
    ]]

    local content, err = db:query(query, { file_id })
    db:release()

    if err then
        return nil, "Failed to get file content: " .. err
    end

    if #content == 0 then
        return nil, "File content not found"
    end

    return content[1].content
end

-- List files by user ID
function file_repo.list_by_user(user_id, limit, offset)
    if not user_id or user_id == "" then
        return nil, "User ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    limit = limit or 100
    offset = offset or 0

    local query = [[
        SELECT file_id, user_id, filename, size, mime_type, status, storage_path, created_at, updated_at
        FROM files
        WHERE user_id = ?
        ORDER BY created_at DESC
        LIMIT ? OFFSET ?
    ]]

    local files, err = db:query(query, { user_id, sql.as.int(limit), sql.as.int(offset) })
    db:release()

    if err then
        return nil, "Failed to list files: " .. err
    end

    return files
end

-- Add a section to a file
function file_repo.add_section(file_id, title, level, order_index, parent_id)
    if not file_id or file_id == "" then
        return nil, "File ID is required"
    end

    if not title then
        title = "" -- Default to empty string if title is nil
    end

    if not level or level < 0 then
        return nil, "Valid heading level is required"
    end

    if not order_index or order_index < 0 then
        return nil, "Valid order index is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local section_id = uuid.v7()

    -- Parameters for the query
    local params = { section_id, file_id, title, sql.as.int(level), sql.as.int(order_index) }

    -- If parent_id is a table, extract the section_id field
    local parent_id_str = parent_id
    if type(parent_id) == "table" then
        if parent_id.section_id then
            parent_id_str = parent_id.section_id
        else
            parent_id_str = nil
        end
    end

    -- Add parent_id if provided, otherwise insert NULL
    if parent_id_str and parent_id_str ~= "" then
        table.insert(params, parent_id_str)
    else
        table.insert(params, sql.as.null())
    end

    -- Insert new section
    local result, err = db:execute(
        "INSERT INTO file_sections (section_id, file_id, title, level, order_index, parent_id) VALUES (?, ?, ?, ?, ?, ?)",
        params
    )

    db:release()

    if err then
        return nil, "Failed to add file section: " .. err
    end

    return section_id, nil  -- Return just the section ID instead of a table
end
-- Get sections for a file
function file_repo.get_sections(file_id)
    if not file_id or file_id == "" then
        return nil, "File ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT section_id, file_id, title, level, order_index, parent_id
        FROM file_sections
        WHERE file_id = ?
        ORDER BY order_index ASC
    ]]

    local sections, err = db:query(query, { file_id })
    db:release()

    if err then
        return nil, "Failed to get file sections: " .. err
    end

    return sections
end

-- Add a chunk to a file
function file_repo.add_chunk(file_id, section_id, content, chunk_type, path, embedding)
    if not file_id or file_id == "" then
        return nil, "File ID is required"
    end

    if not content or content == "" then
        return nil, "Chunk content is required"
    end

    chunk_type = chunk_type or "text"

    -- Encode path as JSON if it's a table
    local path_json
    if type(path) == "table" then
        local ok, encoded = pcall(json.encode, path)
        if not ok then
            return nil, "Failed to encode path: " .. encoded
        end
        path_json = encoded
    else
        path_json = path or "[]"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local chunk_id = uuid.v7()

    -- Parameters for the query
    local params = {
        chunk_id,
        file_id,
        content,
        chunk_type,
        path_json,
        sql.as.int(os.time())
    }

    -- Add section_id if provided, otherwise insert NULL
    if section_id and section_id ~= "" then
        table.insert(params, 2, section_id)
    else
        table.insert(params, 2, sql.as.null())
    end

    -- Add embedding if provided
    if embedding then
        if type(embedding) == "string" then
            table.insert(params, embedding)
        else
            -- Format embedding as JSON array string
            local ok, encoded = pcall(json.encode, embedding)
            if not ok then
                db:release()
                return nil, "Failed to encode embedding: " .. encoded
            end
            table.insert(params, encoded)
        end
    else
        table.insert(params, sql.as.null())
    end

    -- Insert new chunk
    local result, err = db:execute(
        [[INSERT INTO file_chunks
          (chunk_id, file_id, section_id, content, type, path, created_at, embedding)
          VALUES (?, ?, ?, ?, ?, ?, ?, ?)]],
        params
    )

    db:release()

    if err then
        return nil, "Failed to add file chunk: " .. err
    end

    return {
        chunk_id = chunk_id,
        file_id = file_id,
        section_id = section_id,
        type = chunk_type,
        path = path
    }
end

-- Get chunks for a file
function file_repo.get_chunks(file_id, section_id)
    if not file_id or file_id == "" then
        return nil, "File ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query, params

    if section_id and section_id ~= "" then
        query = [[
            SELECT chunk_id, file_id, section_id, content, type, path
            FROM file_chunks
            WHERE file_id = ? AND section_id = ?
            ORDER BY created_at ASC
        ]]
        params = { file_id, section_id }
    else
        query = [[
            SELECT chunk_id, file_id, section_id, content, type, path
            FROM file_chunks
            WHERE file_id = ?
            ORDER BY created_at ASC
        ]]
        params = { file_id }
    end

    local chunks, err = db:query(query, params)
    db:release()

    if err then
        return nil, "Failed to get file chunks: " .. err
    end

    -- Parse JSON paths
    for i, chunk in ipairs(chunks) do
        if chunk.path and chunk.path ~= "" then
            local ok, decoded = pcall(json.decode, chunk.path)
            if ok then
                chunks[i].path = decoded
            end
        end
    end

    return chunks
end

-- Add a document fact
function file_repo.add_fact(file_id, query, original_response, edited_response)
    if not file_id or file_id == "" then
        return nil, "File ID is required"
    end

    if not query or query == "" then
        return nil, "Query is required"
    end

    if not original_response or original_response == "" then
        return nil, "Original response is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local fact_id = uuid.v7()
    local now = os.time()

    -- Parameters for the query
    local params = {
        fact_id,
        file_id,
        query,
        original_response,
        sql.as.int(now),
        sql.as.int(now)
    }

    -- Add edited_response if provided, otherwise insert NULL
    if edited_response and edited_response ~= "" then
        table.insert(params, 5, edited_response)
    else
        table.insert(params, 5, sql.as.null())
    end

    -- Insert new fact
    local result, err = db:execute(
        [[INSERT INTO document_facts
          (fact_id, file_id, query, original_response, edited_response, created_at, updated_at)
          VALUES (?, ?, ?, ?, ?, ?, ?)]],
        params
    )

    db:release()

    if err then
        return nil, "Failed to add document fact: " .. err
    end

    return {
        fact_id = fact_id,
        file_id = file_id,
        query = query,
        original_response = original_response,
        edited_response = edited_response,
        created_at = now,
        updated_at = now
    }
end

-- Update a document fact
function file_repo.update_fact(fact_id, edited_response)
    if not fact_id or fact_id == "" then
        return nil, "Fact ID is required"
    end

    if not edited_response then
        return nil, "Edited response is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    -- Update fact
    local result, err = db:execute(
        "UPDATE document_facts SET edited_response = ?, updated_at = ? WHERE fact_id = ?",
        { edited_response, sql.as.int(os.time()), fact_id }
    )

    db:release()

    if err then
        return nil, "Failed to update document fact: " .. err
    end

    if result.rows_affected == 0 then
        return nil, "Fact not found"
    end

    return {
        fact_id = fact_id,
        edited_response = edited_response,
        updated = true
    }
end

-- Get facts for a file
function file_repo.get_facts(file_id)
    if not file_id or file_id == "" then
        return nil, "File ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    local query = [[
        SELECT fact_id, file_id, query, original_response, edited_response, created_at, updated_at
        FROM document_facts
        WHERE file_id = ?
        ORDER BY created_at DESC
    ]]

    local facts, err = db:query(query, { file_id })
    db:release()

    if err then
        return nil, "Failed to get document facts: " .. err
    end

    return facts
end

-- Delete a file and all related data
function file_repo.delete(file_id)
    if not file_id or file_id == "" then
        return nil, "File ID is required"
    end

    local db, err = get_db()
    if err then
        return nil, err
    end

    -- Begin transaction
    local tx, err = db:begin()
    if err then
        db:release()
        return nil, "Failed to begin transaction: " .. err
    end

    -- Delete document facts
    local result, err = tx:execute("DELETE FROM document_facts WHERE file_id = ?", { file_id })
    if err then
        tx:rollback()
        db:release()
        return nil, "Failed to delete document facts: " .. err
    end

    -- Delete file chunks
    result, err = tx:execute("DELETE FROM file_chunks WHERE file_id = ?", { file_id })
    if err then
        tx:rollback()
        db:release()
        return nil, "Failed to delete file chunks: " .. err
    end

    -- Delete file sections
    result, err = tx:execute("DELETE FROM file_sections WHERE file_id = ?", { file_id })
    if err then
        tx:rollback()
        db:release()
        return nil, "Failed to delete file sections: " .. err
    end

    -- Delete file content
    result, err = tx:execute("DELETE FROM file_content WHERE file_id = ?", { file_id })
    if err then
        tx:rollback()
        db:release()
        return nil, "Failed to delete file content: " .. err
    end

    -- Delete file record
    result, err = tx:execute("DELETE FROM files WHERE file_id = ?", { file_id })
    if err then
        tx:rollback()
        db:release()
        return nil, "Failed to delete file: " .. err
    end

    -- Check if file was found
    if result.rows_affected == 0 then
        tx:rollback()
        db:release()
        return nil, "File not found"
    end

    -- Commit transaction
    local success, err = tx:commit()
    if err then
        tx:rollback()
        db:release()
        return nil, "Failed to commit transaction: " .. err
    end

    db:release()

    return { deleted = true }
end

return file_repo
