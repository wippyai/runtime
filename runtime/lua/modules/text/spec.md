# Lua Text Module Specification

## Overview

The `text` module provides advanced text processing capabilities for Lua, specializing in intelligent text chunking and text comparison for AI applications. It leverages proven algorithms to split large documents into smaller, semantically meaningful chunks while preserving document structure and metadata, and provides sophisticated diff functionality for comparing and patching text content.

The module is optimized for document processing pipelines, embedding generation, AI content preparation workflows, version control operations, and text analysis tasks.

---

## Module Interface

### Module Loading

```lua
local text = require("text")
```

### Error Handling

All functions in the module follow a consistent error handling pattern, returning two values:

1. The result value (or nil if an error occurred)
2. An error message string (or nil if operation was successful)

Example:

```lua
local splitter, err = text.splitter.recursive({chunk_size = 1000})
if err then
    error("Failed to create splitter: " .. err)
end

local differ, err = text.diff.new({diff_timeout = 1.0})
if err then
    error("Failed to create differ: " .. err)
end
```

---

## Splitter Sub-Module

The `text.splitter` sub-module provides intelligent text chunking capabilities with different algorithms optimized for various content types.

### `text.splitter.recursive(options)`

**Description:**  
Creates a recursive character splitter that intelligently splits text by trying different separators in order of preference. Best for general-purpose text processing.

**Parameters:**

- **`options`** (table, optional): Configuration options:
  - **`chunk_size`** (number): Maximum chunk size in characters (default: 4000)
  - **`chunk_overlap`** (number): Number of overlapping characters between chunks (default: 200)
  - **`separators`** (array): Separators to try in order (default: `{"\n\n", "\n", " ", ""}`)
  - **`keep_separator`** (boolean): Whether to keep separators in the text (default: false)

**Returns:**

- **On success:** A TextSplitter object with methods for splitting text
- **On failure:** `nil` and an error message string

**Example:**

```lua
local splitter, err = text.splitter.recursive({
    chunk_size = 1000,
    chunk_overlap = 150,
    separators = {"\n\n", "\n", ". ", " ", ""},
    keep_separator = false
})
if err then
    error("Failed to create splitter: " .. err)
end
```

### `text.splitter.markdown(options)`

**Description:**  
Creates a markdown-aware splitter that preserves document structure while splitting. Ideal for markdown documents, documentation, and structured text.

**Parameters:**

- **`options`** (table, optional): Configuration options:
  - **`chunk_size`** (number): Maximum chunk size in characters (default: 4000)
  - **`chunk_overlap`** (number): Number of overlapping characters between chunks (default: 200)
  - **`code_blocks`** (boolean): Whether to include code blocks in output (default: false)
  - **`reference_links`** (boolean): Whether to resolve reference links (default: false)
  - **`heading_hierarchy`** (boolean): Whether to preserve heading hierarchy in chunks (default: false)
  - **`join_table_rows`** (boolean): Whether to join table rows in single chunks (default: false)

**Returns:**

- **On success:** A TextSplitter object with methods for splitting text
- **On failure:** `nil` and an error message string

**Example:**

```lua
local splitter, err = text.splitter.markdown({
    chunk_size = 1000,
    chunk_overlap = 150,
    code_blocks = true,
    heading_hierarchy = true,
    join_table_rows = false
})
if err then
    error("Failed to create splitter: " .. err)
end
```

---

## Diff Sub-Module

The `text.diff` sub-module provides sophisticated text comparison, diff generation, and patch application capabilities based on the Google Diff-Match-Patch algorithm.

### `text.diff.new(options)`

**Description:**  
Creates a text differ that can compare texts, generate diffs, create patches, and apply changes. Ideal for version control workflows, document comparison, and change tracking.

**Parameters:**

- **`options`** (table, optional): Configuration options:
  - **`diff_timeout`** (number): Timeout in seconds for diff computation (default: 1.0)
  - **`diff_edit_cost`** (number): Cost of an edit operation for diff quality (default: 4)
  - **`match_threshold`** (number): Match threshold (0.0 = perfection, 1.0 = very loose, default: 0.5)
  - **`match_distance`** (number): How far to search for a match (default: 1000)
  - **`patch_delete_threshold`** (number): Delete threshold for patch application (default: 0.5)
  - **`patch_margin`** (number): Number of context lines around patches (default: 4)

**Returns:**

- **On success:** A Differ object with methods for text comparison
- **On failure:** `nil` and an error message string

**Example:**

```lua
local differ, err = text.diff.new({
    diff_timeout = 1.0,
    match_threshold = 0.5,
    patch_margin = 4
})
if err then
    error("Failed to create differ: " .. err)
end
```

---

## TextSplitter Object Methods

Once created, TextSplitter objects provide the following methods:

### `splitter:split_text(content)`

**Description:**  
Splits a single text string into chunks according to the splitter's configuration.

**Parameters:**

- **`content`** (string): The text content to split

**Returns:**

- **On success:** Array of text chunks (strings)
- **On failure:** `nil` and an error message string

**Example:**

```lua
local content = "This is a long document that needs to be split into smaller chunks for processing..."
local chunks, err = splitter:split_text(content)
if err then
    error("Failed to split text: " .. err)
end

for i, chunk in ipairs(chunks) do
    print("Chunk " .. i .. ": " .. chunk)
end
```

### `splitter:split_batch(pages)`

**Description:**  
Splits multiple text items with associated metadata in a single operation. Each input item's metadata is preserved and attached to all chunks generated from that item.

**Parameters:**

- **`pages`** (array): Array of page objects, each containing:
  - **`content`** (string): The text content to split
  - **`metadata`** (table): Metadata to preserve with each chunk

**Returns:**

- **On success:** Array of chunk objects, each containing:
  - **`content`** (string): The chunk text
  - **`metadata`** (table): The original metadata from the source page
- **On failure:** `nil` and an error message string

**Example:**

```lua
local pages = {
    {
        content = "Content from page 1 of the document...",
        metadata = {page_number = 1, document_id = "doc123", source = "manual.pdf"}
    },
    {
        content = "Content from page 2 of the document...",
        metadata = {page_number = 2, document_id = "doc123", source = "manual.pdf"}
    }
}

local chunks, err = splitter:split_batch(pages)
if err then
    error("Failed to split batch: " .. err)
end

for i, chunk in ipairs(chunks) do
    print("Chunk content: " .. chunk.content)
    print("From page: " .. chunk.metadata.page_number)
    print("Document: " .. chunk.metadata.document_id)
end
```

---

## Differ Object Methods

Once created, Differ objects provide the following methods:

### `differ:compare(text1, text2)`

**Description:**  
Compares two text strings and returns an array of diff operations describing the changes needed to transform text1 into text2.

**Parameters:**

- **`text1`** (string): The original text
- **`text2`** (string): The modified text

**Returns:**

- **On success:** Array of diff objects, each containing:
  - **`operation`** (string): The operation type ("equal", "delete", "insert")
  - **`text`** (string): The text content for this operation
- **On failure:** `nil` and an error message string

**Example:**

```lua
local old_text = "The quick brown fox"
local new_text = "The quick red fox"

local diffs, err = differ:compare(old_text, new_text)
if err then
    error("Failed to compare texts: " .. err)
end

for i, diff in ipairs(diffs) do
    print(string.format("%s: '%s'", diff.operation, diff.text))
end
-- Output:
-- equal: 'The quick '
-- delete: 'brown'
-- insert: 'red'
-- equal: ' fox'
```

### `differ:pretty_text(diffs)`

**Description:**  
Converts an array of diff operations into a human-readable unified diff format, similar to git diff output.

**Parameters:**

- **`diffs`** (array): Array of diff objects from `compare()`

**Returns:**

- **On success:** Formatted diff string
- **On failure:** `nil` and an error message string

**Example:**

```lua
local diffs, err = differ:compare("Hello world", "Hello beautiful world")
if err then
    error("Compare failed: " .. err)
end

local pretty, err = differ:pretty_text(diffs)
if err then
    error("Pretty text failed: " .. err)
end

print(pretty)
-- Output shows insertions/deletions in readable format
```

> Use pretty_html for same purposes.

### `differ:patch_make(text1, text2)`

**Description:**  
Creates patches that can be used to transform text1 into text2. Patches are compact representations of changes that can be stored or transmitted.

**Parameters:**

- **`text1`** (string): The original text
- **`text2`** (string): The target text

**Returns:**

- **On success:** Array of patch objects
- **On failure:** `nil` and an error message string

**Example:**

```lua
local patches, err = differ:patch_make("Hello world", "Hello beautiful world")
if err then
    error("Failed to create patches: " .. err)
end

print("Created " .. #patches .. " patches")
```

### `differ:patch_apply(patches, text)`

**Description:**  
Applies an array of patches to a text string, attempting to transform it according to the patches.

**Parameters:**

- **`patches`** (array): Array of patch objects from `patch_make()`
- **`text`** (string): The text to apply patches to

**Returns:**

- **On success:** The patched text and a boolean indicating if all patches applied successfully
- **On failure:** `nil` and an error message string

**Example:**

```lua
local original = "Hello world"
local modified = "Hello beautiful world"

-- Create patches
local patches, err = differ:patch_make(original, modified)
if err then
    error("Patch creation failed: " .. err)
end

-- Apply patches
local result, success = differ:patch_apply(patches, original)
if not success then
    print("Warning: Some patches failed to apply")
end

print("Result: " .. result)
print("Matches target: " .. tostring(result == modified))
```

### `differ:summarize(diffs)`

**Description:**  
Analyzes an array of diff operations and returns statistics about the changes.

**Parameters:**

- **`diffs`** (array): Array of diff objects from `compare()`

**Returns:**

- Summary table containing:
  - **`insertions`** (number): Total characters inserted
  - **`deletions`** (number): Total characters deleted
  - **`equals`** (number): Total characters unchanged

**Example:**

```lua
local diffs, err = differ:compare("The old text", "The new text")
if err then
    error("Compare failed: " .. err)
end

local summary = differ:summarize(diffs)
print("Changes: " .. summary.deletions .. " deletions, " .. summary.insertions .. " insertions")
print("Unchanged: " .. summary.equals .. " characters")
```

---

## Best Practices

### 1. Choose the Right Tool

```lua
-- For document chunking (AI/ML workflows)
local splitter = text.splitter.recursive({
    chunk_size = 1000,
    chunk_overlap = 150
})

-- For version control and change tracking
local differ = text.diff.new({
    diff_timeout = 1.0,
    match_threshold = 0.5
})

-- For markdown documentation
local md_splitter = text.splitter.markdown({
    heading_hierarchy = true,
    code_blocks = true
})
```

### 2. Configure for Your Use Case

```lua
-- For embeddings with token limits
local embedding_splitter = text.splitter.recursive({
    chunk_size = 800,    -- Leave room for metadata
    chunk_overlap = 100
})

-- For code comparison (strict matching)
local code_differ = text.diff.new({
    match_threshold = 0.0,  -- Exact matches only
    diff_edit_cost = 10     -- Higher cost for edits
})

-- For fuzzy document comparison
local doc_differ = text.diff.new({
    match_threshold = 0.8,  -- More flexible matching
    diff_timeout = 2.0      -- Allow more time for complex diffs
})
```

### 3. Batch Processing for Efficiency

```lua
-- Process multiple files with diff analysis
local function analyze_file_changes(file_pairs)
    local differ, err = text.diff.new()
    if err then
        error("Failed to create differ: " .. err)
    end
    
    local results = {}
    for _, pair in ipairs(file_pairs) do
        local diffs, err = differ:compare(pair.old_content, pair.new_content)
        if err then
            print("Error processing " .. pair.filename .. ": " .. err)
        else
            local summary = differ:summarize(diffs)
            table.insert(results, {
                filename = pair.filename,
                changes = summary.deletions + summary.insertions,
                summary = summary
            })
        end
    end
    
    return results
end
```

### 4. Error Handling and Validation

```lua
local function safe_text_processing(old_text, new_text)
    local differ, err = text.diff.new({diff_timeout = 5.0})
    if err then
        return nil, "Failed to create differ: " .. err
    end
    
    -- Validate inputs
    if type(old_text) ~= "string" or type(new_text) ~= "string" then
        return nil, "Both inputs must be strings"
    end
    
    -- Perform comparison
    local diffs, err = differ:compare(old_text, new_text)
    if err then
        return nil, "Comparison failed: " .. err
    end
    
    -- Generate output
    local pretty, err = differ:pretty_text(diffs)
    if err then
        return nil, "Pretty text generation failed: " .. err
    end
    
    return {
        diffs = diffs,
        pretty = pretty,
        summary = differ:summarize(diffs)
    }
end
```

### 5. Memory Efficiency for Large Documents

```lua
-- Process large documents in chunks for diff analysis
local function diff_large_documents(doc1, doc2, chunk_size)
    chunk_size = chunk_size or 10000
    local splitter, err = text.splitter.recursive({
        chunk_size = chunk_size,
        chunk_overlap = 200
    })
    if err then
        error("Failed to create splitter: " .. err)
    end
    
    local differ, err = text.diff.new()
    if err then
        error("Failed to create differ: " .. err)
    end
    
    -- Split both documents
    local chunks1, err = splitter:split_text(doc1)
    if err then
        error("Failed to split document 1: " .. err)
    end
    
    local chunks2, err = splitter:split_text(doc2)
    if err then
        error("Failed to split document 2: " .. err)
    end
    
    -- Compare corresponding chunks
    local all_diffs = {}
    local max_chunks = math.max(#chunks1, #chunks2)
    
    for i = 1, max_chunks do
        local chunk1 = chunks1[i] or ""
        local chunk2 = chunks2[i] or ""
        
        local diffs, err = differ:compare(chunk1, chunk2)
        if err then
            print("Warning: Failed to compare chunk " .. i .. ": " .. err)
        else
            table.insert(all_diffs, {
                chunk_index = i,
                diffs = diffs,
                summary = differ:summarize(diffs)
            })
        end
    end
    
    return all_diffs
end
```

---

## Complete Example Usage

### Document Processing with Change Tracking

```lua
local text = require("text")

-- Create tools
local splitter, err = text.splitter.recursive({
    chunk_size = 1000,
    chunk_overlap = 150
})
if err then
    error("Failed to create splitter: " .. err)
end

local differ, err = text.diff.new({
    diff_timeout = 1.0,
    match_threshold = 0.5
})
if err then
    error("Failed to create differ: " .. err)
end

-- Process document versions
local function analyze_document_changes(old_doc, new_doc, document_id)
    -- First, compare overall documents
    local doc_diffs, err = differ:compare(old_doc, new_doc)
    if err then
        error("Failed to compare documents: " .. err)
    end
    
    local doc_summary = differ:summarize(doc_diffs)
    print(string.format("Document %s: %d chars changed (%d del, %d ins)", 
        document_id, doc_summary.deletions + doc_summary.insertions,
        doc_summary.deletions, doc_summary.insertions))
    
    -- Split both versions for detailed analysis
    local old_chunks, err = splitter:split_text(old_doc)
    if err then
        error("Failed to split old document: " .. err)
    end
    
    local new_chunks, err = splitter:split_text(new_doc)
    if err then
        error("Failed to split new document: " .. err)
    end
    
    -- Analyze changes at chunk level
    local chunk_changes = {}
    local max_chunks = math.max(#old_chunks, #new_chunks)
    
    for i = 1, max_chunks do
        local old_chunk = old_chunks[i] or ""
        local new_chunk = new_chunks[i] or ""
        
        if old_chunk ~= new_chunk then
            local chunk_diffs, err = differ:compare(old_chunk, new_chunk)
            if err then
                print("Warning: Failed to compare chunk " .. i)
            else
                local chunk_summary = differ:summarize(chunk_diffs)
                table.insert(chunk_changes, {
                    chunk_index = i,
                    summary = chunk_summary,
                    pretty_diff = differ:pretty_text(chunk_diffs)
                })
            end
        end
    end
    
    return {
        overall_summary = doc_summary,
        chunk_changes = chunk_changes,
        total_chunks = max_chunks
    }
end

-- Usage example
local old_version = [[
# Document Title

This is the introduction paragraph.

## Section 1

Content of section 1 with some details.

## Section 2

Content of section 2 with more information.
]]

local new_version = [[
# Document Title

This is the updated introduction paragraph with more details.

## Section 1

Enhanced content of section 1 with additional details and examples.

## Section 2

Content of section 2 with more information.

## Section 3

This is a new section with additional content.
]]

local analysis = analyze_document_changes(old_version, new_version, "doc_v2")
print("Analysis complete: " .. #analysis.chunk_changes .. " chunks changed")
```

### Version Control Workflow

```lua
local text = require("text")

-- Create differ for patch-based workflow
local differ, err = text.diff.new({
    patch_margin = 3,
    match_threshold = 0.3
})
if err then
    error("Failed to create differ: " .. err)
end

-- Simulate git-like operations
local function create_commit(old_content, new_content, commit_message)
    -- Generate patches (like git diff)
    local patches, err = differ:patch_make(old_content, new_content)
    if err then
        error("Failed to create patches: " .. err)
    end
    
    -- Generate human-readable diff
    local diffs, err = differ:compare(old_content, new_content)
    if err then
        error("Failed to generate diffs: " .. err)
    end
    
    local pretty_diff, err = differ:pretty_text(diffs)
    if err then
        error("Failed to generate pretty diff: " .. err)
    end
    
    return {
        message = commit_message,
        patches = patches,
        diff = pretty_diff,
        summary = differ:summarize(diffs)
    }
end

local function apply_commit(commit, base_content)
    -- Apply patches (like git apply)
    local result, success = differ:patch_apply(commit.patches, base_content)
    
    return result, success
end

-- Example usage
local original = "function hello() {\n    console.log('Hello');\n}"
local modified = "function hello() {\n    console.log('Hello World!');\n    return true;\n}"

local commit = create_commit(original, modified, "Add return value and update message")
print("Commit created:")
print("Message: " .. commit.message)
print("Changes: " .. commit.summary.insertions .. " insertions, " .. commit.summary.deletions .. " deletions")
print("\nDiff:")
print(commit.diff)

-- Apply to another branch
local result, success = apply_commit(commit, original)
if success then
    print("\nPatch applied successfully:")
    print(result)
else
    print("Patch application failed")
end
```