```lua
local fs = require('fs')

-- Constants
fs.seek = { SET = "set", CUR = "cur", END = "end" }
fs.type = { FILE = "file", DIR = "directory" }

-- Get filesystem instance (raises on invalid name or access denied)
local myfs = fs.default()  -- get default fs
local datafs = fs.get("data") -- get named fs

-- Working directory operations (raise on invalid path/permissions)
myfs:chdir(path)        
local cwd = myfs:pwd()  

-- File objects (raises on invalid mode, path, permissions)
local file = myfs:open(path, mode) -- mode: "r", "w", "a"

-- File operations (return nil, err on EOF/partial ops)
local data, err = file:read(n)     -- nil, "EOF" on end of file
local ok, err = file:write(data)   -- nil, err on partial write
local pos, err = file:seek(whence, offset)

-- Must close (raises on double close)
file:close()     

-- File info (raises on non-existent path)
local info = file:stat()
local info = myfs:stat(path)
info.name       -- base name
info.size       -- bytes  
info.mode       -- file mode
info.modified   -- mod time
info.is_dir     -- bool
info.type       -- "file" or "directory"

-- Directory iteration (raises on permissions)
for entry in myfs:readdir(path) do
  entry.name     
  entry.type      
  local info = entry:info()
end

-- Write operations (raise on permissions/invalid paths)
myfs:mkdir(path)          -- raise on existing path
myfs:remove(path)         -- raise on non-empty dir

-- Example usage with error handling
local function example()
  local myfs = assert(fs.default())
  
  -- Operations that raise
  myfs:chdir("data")  -- raises on invalid path
  
  -- Operations that return status
  local file = assert(myfs:open("config.json", "r"))
  local data, err = file:read()
  if err then
    -- handle EOF or read error
    return nil, err
  end
  
  local ok, err = file:write("new data")
  if not ok then
    -- handle partial write
    return nil, err
  end
  
  -- Must close
  file:close()
end
```