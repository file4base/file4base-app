-- File4Base System Catalog Migration 010 (PostgreSQL & MariaDB)
--
-- The FileMaker ids of migrated fields deleted since the migration (#191).
-- A FileMaker Import Records step reads its source fields by position, the
-- field's place among its table's fields in creation order. Keeping the id
-- of a deleted field keeps its place, so the positions after it do not
-- shift onto the next field, and a step that reads it is told so.
--
--   sys_deleted_source_fields.table_id          the table the field was in
--   sys_deleted_source_fields.source_field_id   the id FileMaker gave it
--
-- The server applies this itself (schema.EnsureSystemTables).

CREATE TABLE IF NOT EXISTS sys_deleted_source_fields (
    table_id VARCHAR(36) NOT NULL REFERENCES sys_tables(id) ON DELETE CASCADE,
    source_field_id VARCHAR(32) NOT NULL,
    PRIMARY KEY (table_id, source_field_id)
);
