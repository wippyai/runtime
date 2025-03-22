-- Simple queue implementation for managing task payloads
local queue = {}
queue.__index = queue

-- Create a new queue
function queue.new()
    local self = setmetatable({}, queue)
    self.items = {}
    self.head = 1
    self.tail = 0
    self.size = 0
    return self
end

-- Add an item to the queue
function queue:enqueue(item)
    self.tail = self.tail + 1
    self.items[self.tail] = item
    self.size = self.size + 1
    return true
end

-- Remove and return the next item from the queue
function queue:dequeue()
    if self.size <= 0 then
        return nil, "Queue is empty"
    end

    local item = self.items[self.head]
    self.items[self.head] = nil
    self.head = self.head + 1
    self.size = self.size - 1

    -- Reset indices if queue is empty to avoid growing indices indefinitely
    if self.size == 0 then
        self.head = 1
        self.tail = 0
    end
    return item
end

-- Check if the queue is empty
function queue:is_empty()
    return self.size == 0
end

-- Get the current size of the queue
function queue:get_size()
    return self.size
end

-- Clear all items from the queue
function queue:clear()
    self.items = {}
    self.head = 1
    self.tail = 0
    self.size = 0
end

-- Peek at the next item without removing it
function queue:peek()
    if self.size <= 0 then
        return nil
    end
    return self.items[self.head]
end

return queue
