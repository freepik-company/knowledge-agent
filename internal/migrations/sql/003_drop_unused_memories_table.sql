-- Drop unused memories table (adk-utils-go uses memory_entries instead)
-- The adk-utils-go library creates and manages memory_entries automatically
DROP TABLE IF EXISTS memories CASCADE;

-- Drop unused functions
DROP FUNCTION IF EXISTS search_memories(vector(768), VARCHAR(255), INT, FLOAT);
DROP FUNCTION IF EXISTS search_thread_memories(vector(768), VARCHAR(255), VARCHAR(255), INT, FLOAT);
DROP FUNCTION IF EXISTS get_thread_summary(VARCHAR(255), VARCHAR(255));
DROP FUNCTION IF EXISTS is_thread_ingested(VARCHAR(255), VARCHAR(255));

-- Drop unused views
DROP VIEW IF EXISTS thread_memories;

-- Note: memory_entries table is auto-created by adk-utils-go library at runtime
-- No migration needed for it
