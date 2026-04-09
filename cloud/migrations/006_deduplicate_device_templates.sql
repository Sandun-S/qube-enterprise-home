-- 006_deduplicate_device_templates.sql
-- Removes duplicate device_templates rows (caused by running seed files more than once)
-- and adds a unique constraint to prevent recurrence.

-- Keep only the earliest-inserted global row per (protocol, name) pair
DELETE FROM device_templates
WHERE is_global = TRUE
  AND id NOT IN (
    SELECT DISTINCT ON (protocol, name) id
    FROM device_templates
    WHERE is_global = TRUE
    ORDER BY protocol, name, created_at ASC
);

-- Prevent future global template duplicates
ALTER TABLE device_templates
    ADD CONSTRAINT device_templates_global_protocol_name_key
    UNIQUE (protocol, name, is_global)
    DEFERRABLE INITIALLY DEFERRED;
