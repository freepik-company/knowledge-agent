-- Indexes for Slack-specific metadata queries
-- These optimize common queries for thread and channel lookups

-- Index for thread_ts lookups (check if thread already ingested)
CREATE INDEX IF NOT EXISTS idx_memories_thread_ts
ON memories ((metadata->>'thread_ts'))
WHERE metadata->>'thread_ts' IS NOT NULL;

-- Index for channel_id lookups
CREATE INDEX IF NOT EXISTS idx_memories_channel_id
ON memories ((metadata->>'channel_id'))
WHERE metadata->>'channel_id' IS NOT NULL;

-- Composite index for thread and channel queries
CREATE INDEX IF NOT EXISTS idx_memories_thread_channel
ON memories ((metadata->>'thread_ts'), (metadata->>'channel_id'))
WHERE metadata->>'thread_ts' IS NOT NULL AND metadata->>'channel_id' IS NOT NULL;

-- Index for timestamp range queries
-- Note: Using text-based index instead of timestamp cast to avoid IMMUTABLE function requirement
CREATE INDEX IF NOT EXISTS idx_memories_timestamp_start
ON memories ((metadata->>'timestamp_start'))
WHERE metadata->>'timestamp_start' IS NOT NULL;

-- View for easy thread metadata access
CREATE OR REPLACE VIEW thread_memories AS
SELECT
    id,
    content,
    metadata->>'thread_ts' AS thread_ts,
    metadata->>'channel_id' AS channel_id,
    metadata->>'channel_name' AS channel_name,
    (metadata->>'participants')::jsonb AS participants,
    metadata->>'timestamp_start' AS timestamp_start,
    metadata->>'timestamp_end' AS timestamp_end,
    (metadata->>'message_count')::int AS message_count,
    (metadata->>'has_code')::boolean AS has_code,
    (metadata->>'has_links')::boolean AS has_links,
    created_at,
    updated_at
FROM memories
WHERE metadata->>'thread_ts' IS NOT NULL;

-- Function to check if thread already ingested
CREATE OR REPLACE FUNCTION is_thread_ingested(
    p_thread_ts VARCHAR(255),
    p_channel_id VARCHAR(255)
)
RETURNS BOOLEAN AS $$
DECLARE
    thread_exists BOOLEAN;
BEGIN
    SELECT EXISTS(
        SELECT 1
        FROM memories
        WHERE metadata->>'thread_ts' = p_thread_ts
        AND metadata->>'channel_id' = p_channel_id
    ) INTO thread_exists;

    RETURN thread_exists;
END;
$$ LANGUAGE plpgsql;

-- Function to get thread summary
CREATE OR REPLACE FUNCTION get_thread_summary(
    p_thread_ts VARCHAR(255),
    p_channel_id VARCHAR(255)
)
RETURNS TABLE (
    memory_count BIGINT,
    total_messages INT,
    participants JSONB,
    first_message TIMESTAMP,
    last_message TIMESTAMP,
    has_code BOOLEAN,
    has_links BOOLEAN
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        COUNT(*) AS memory_count,
        MAX((metadata->>'message_count')::int) AS total_messages,
        jsonb_agg(DISTINCT metadata->'participants') AS participants,
        MIN(((metadata->>'timestamp_start')::timestamp)) AS first_message,
        MAX(((metadata->>'timestamp_end')::timestamp)) AS last_message,
        bool_or((metadata->>'has_code')::boolean) AS has_code,
        bool_or((metadata->>'has_links')::boolean) AS has_links
    FROM memories
    WHERE metadata->>'thread_ts' = p_thread_ts
    AND metadata->>'channel_id' = p_channel_id;
END;
$$ LANGUAGE plpgsql;

-- Function to search memories within a specific thread
CREATE OR REPLACE FUNCTION search_thread_memories(
    query_embedding vector(768),
    p_thread_ts VARCHAR(255),
    p_channel_id VARCHAR(255),
    limit_count INT DEFAULT 5,
    similarity_threshold FLOAT DEFAULT 0.7
)
RETURNS TABLE (
    id INT,
    content TEXT,
    metadata JSONB,
    similarity FLOAT,
    created_at TIMESTAMP WITH TIME ZONE
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        m.id,
        m.content,
        m.metadata,
        1 - (m.embedding <=> query_embedding) AS similarity,
        m.created_at
    FROM memories m
    WHERE
        m.metadata->>'thread_ts' = p_thread_ts
        AND m.metadata->>'channel_id' = p_channel_id
        AND m.embedding IS NOT NULL
        AND (1 - (m.embedding <=> query_embedding)) >= similarity_threshold
    ORDER BY m.embedding <=> query_embedding
    LIMIT limit_count;
END;
$$ LANGUAGE plpgsql;

-- Add comments
COMMENT ON VIEW thread_memories IS 'Simplified view of memories with extracted Slack metadata';
COMMENT ON FUNCTION is_thread_ingested IS 'Check if a Slack thread has already been ingested';
COMMENT ON FUNCTION get_thread_summary IS 'Get summary statistics for an ingested thread';
COMMENT ON FUNCTION search_thread_memories IS 'Search memories within a specific Slack thread';
