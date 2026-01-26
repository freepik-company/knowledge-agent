-- Drop unused memories table (adk-utils-go uses memory_entries instead)
-- The adk-utils-go library creates and manages memory_entries automatically
DROP TABLE IF EXISTS memories CASCADE;

-- Drop unused function
DROP FUNCTION IF EXISTS search_memories(vector(768), VARCHAR(255), INT, FLOAT);

-- Add comment explaining the table structure
COMMENT ON TABLE memory_entries IS 'Memory storage used by adk-utils-go library. Auto-created and managed by the library.';
