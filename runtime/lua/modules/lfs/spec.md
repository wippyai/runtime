```
# Lua File System Module Specification

## Overview

The `lfs` module provides a Lua interface for interacting with the underlying operating system's file system. It offers functions for file and directory manipulation, such as attribute retrieval, directory creation and removal, current working directory management, symbolic link handling, and more.

## Module Interface

### Module Loading

```lua
local lfs = require("lfs")
```

### Global Functions

#### lfs.attributes(filepath: string [, aname: string])

Retrieves information about a file or directory.

- **filepath:** The path to the file or directory.
- **aname:** (Optional) The specific attribute to retrieve.

Returns:

- If `aname` is not provided, returns a table containing the following attributes:
    - `dev`: Device ID.
    - `ino`: Inode number.
    - `mode`:  Type of the file: "file", "directory", "link", "socket", "named pipe", "char device", "block device",
      or "other".
    - `nlink`: Number of hard links to the file.
    - `uid`: User ID of the file's owner.
    - `gid`: Group ID of the file's owner.
    - `rdev`: Device ID (if special file).
    - `access`: Last access time (as a number representing seconds).
    - `modification`: Last modification time (as a number representing seconds).
    - `change`: Last status change time (as a number representing seconds).
    - `size`: Total size of the file in bytes.
    - `blocks`: Number of blocks allocated for the file.
    - `blksize`: Optimal block size for I/O operations.
    - `target`: If the file is a symbolic link and its target can be evaluated, this field holds the target path as a
      string.

- If `aname` is provided, returns the value of the specified attribute.
- Returns `nil` and an error message if an error occurs.

#### lfs.chdir(path: string)

Changes the current working directory.

- **path:** The path to the new working directory.

Returns:

- `true` on success.
- `nil` and an error message on failure.

#### lfs.currentdir()

Gets the current working directory.

Returns:

- The path to the current working directory as a string.
- `nil` and an error message on failure.

#### lfs.dir(path: string)

Returns an iterator function that yields the names of entries in a directory.

- **path:** The path to the directory.

Returns:

- An iterator function.
- The directory file handle as the second return value.
- In case of error during iteration, the iterator function returns `nil`.

#### lfs.link(old: string, new: string [, symlink: boolean])

Creates a link. If `symlink` is true, creates a symbolic link; otherwise, creates a hard link.

- **old:** Path to the existing file.
- **new:** Path to the new link.
- **symlink:** (Optional) If `true`, creates a symbolic link. Defaults to `false`.

Returns:

- `true` on success.
- `nil` and an error message on failure.

#### lfs.mkdir(dirname: string)

Creates a new directory.

- **dirname:** The path to the new directory.

Returns:

- `true` on success.
- `nil` and an error message on failure.

#### lfs.rmdir(dirname: string)

Removes an empty directory.

- **dirname:** The path to the directory to remove.

Returns:

- `true` on success.
- `nil` and an error message on failure.

#### lfs.symlinkattributes(filepath: string [, aname: string])

Retrieves information about a symbolic link itself (not the file it points to).

- **filepath:** The path to the symbolic link.
- **aname:** (Optional) The specific attribute to retrieve.

Returns:

- Similar to `lfs.attributes`, but operates on the link itself.
- Returns `nil` and an error message if an error occurs.

#### lfs.touch(filepath: string [, atime: number [, mtime: number]])

Sets the access and modification times of a file. If the file does not exist, it is created.

- **filepath:** The path to the file.
- **atime:** (Optional) The new access time (in seconds). Defaults to the current time.
- **mtime:** (Optional) The new modification time (in seconds). Defaults to `atime`.

Returns:

- `true` on success.
- `nil` and an error message on failure.

#### lfs.lock_dir()

#### lfs.lock()

#### lfs.setmode()

#### lfs.unlock()

These functions are currently unimplemented and will raise an error if called.

## Error Handling

- Most functions return `nil` and an error message string on failure.
- `lfs.dir()` returns an iterator function. If an error occurs during iteration the iterator will return `nil` in the
  next iteration.
- `lfs.currentdir()`, on error, returns `nil` and an error message.
- `lfs.attributes()` and `lfs.symlinkattributes()` return `nil` and an error message if an attribute is not found or if
  an error occurs.

## Behavior

- The `lfs.attributes()` and `lfs.symlinkattributes()` functions provide detailed file information similar to the `stat`
  system call.
- The `lfs.dir()` function returns an iterator for traversing directory contents.
- `lfs.touch()` creates a file if it doesn't exist before updating timestamps.
- Directory paths can be relative or absolute.
- The `mode` attribute returned by `lfs.attributes()` and `lfs.symlinkattributes()` indicates the file type (e.g., "
  file", "directory", "link").

## Thread Safety

- Thread safety depends on the underlying operating system's file system operations.

## Best Practices

- Always check for errors returned by `lfs` function.
- Use the iterator returned by `lfs.dir()` to efficiently process directory contents.
- Be aware that creating symbolic links might require appropriate permissions.
- Use absolute paths when possible to avoid ambiguity.

## Example Usage

```lua
local lfs = require("lfs")

-- Get current working directory
local cwd = lfs.currentdir()
print("Current directory:", cwd)

-- Change current working directory
if lfs.chdir("..") then
  print("Changed directory to:", lfs.currentdir())
end

-- Create a directory
if lfs.mkdir("mydir") then
  print("Directory created successfully")
end

-- List files in a directory
for file in lfs.dir(cwd) do
  print("File:", file)
end

-- Get file attributes
local attr = lfs.attributes("myfile.txt")
if attr then
  print("File size:", attr.size)
  print("File mode:", attr.mode)
else
  print("Error getting file attributes")
end

-- Create a symbolic link (if supported by the OS)
if lfs.link("myfile.txt", "mylink.txt", true) then
    print("Symbolic link created")
end

-- Touch a file (create it if it doesn't exist and update timestamps)
if lfs.touch("newfile.txt") then
    print("File touched/created successfully")
end

-- Remove a directory
if lfs.rmdir("mydir") then
  print("Directory removed successfully")
end
```
