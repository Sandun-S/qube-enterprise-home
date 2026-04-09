-- 005_deduplicate_reader_templates.sql
-- Removes duplicate reader_templates rows (caused by running seed files more than once)
-- and adds a unique constraint to prevent recurrence.

-- Keep only the earliest-inserted row per (protocol, image_suffix) pair
DELETE FROM reader_templates
WHERE id NOT IN (
    SELECT DISTINCT ON (protocol, image_suffix) id
    FROM reader_templates
    ORDER BY protocol, image_suffix, created_at ASC
);

-- Prevent future duplicates
ALTER TABLE reader_templates
    ADD CONSTRAINT reader_templates_protocol_image_suffix_key
    UNIQUE (protocol, image_suffix);
