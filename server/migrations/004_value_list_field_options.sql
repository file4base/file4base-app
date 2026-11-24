-- File4Base System Catalog Migration 004 (PostgreSQL & MariaDB)
--
-- Options of a value list taken from a field, as FileMaker's Specify Fields
-- for Value List gives them. Every column is nullable; a list without them
-- offers its first field's values, sorted by them, from every record.
--
--   second_column_id       a second field of the same table, shown beside the
--                          first; choosing an entry stores the first field's
--                          value. Deleting the field clears it.
--   show_second_only       show the second field's value in place of the
--                          first's.
--   sort_by                'first' or 'second'.
--   related_occurrence_id  include only the values of the records related to
--                          the current record of this table occurrence.
--   source_occurrence_id   the occurrence of the list's table those related
--                          values are read through.
--
-- The server applies this itself (schema.EnsureSystemTables, and the first
-- time a value list is read or written on an older database), with the same
-- statements and the same constraint names: every foreign key is named
-- fk_value_list_<column>, so applying this file and starting the server never
-- gives a column two keys.

ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS show_second_only BOOLEAN;
ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS sort_by VARCHAR(16);

-- PostgreSQL: IF NOT EXISTS skips the column together with its key.
ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS second_column_id VARCHAR(36)
    CONSTRAINT fk_value_list_second_column_id REFERENCES sys_columns(id) ON DELETE SET NULL;
ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS source_occurrence_id VARCHAR(36)
    CONSTRAINT fk_value_list_source_occurrence_id REFERENCES sys_table_occurrences(id) ON DELETE CASCADE;
ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS related_occurrence_id VARCHAR(36)
    CONSTRAINT fk_value_list_related_occurrence_id REFERENCES sys_table_occurrences(id) ON DELETE CASCADE;

-- MariaDB, in place of the three statements above: a REFERENCES in the column
-- definition would be added again on every run, so the key is added apart.
--
-- ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS second_column_id VARCHAR(36),
--     ADD CONSTRAINT fk_value_list_second_column_id FOREIGN KEY IF NOT EXISTS (second_column_id)
--     REFERENCES sys_columns(id) ON DELETE SET NULL;
-- ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS source_occurrence_id VARCHAR(36),
--     ADD CONSTRAINT fk_value_list_source_occurrence_id FOREIGN KEY IF NOT EXISTS (source_occurrence_id)
--     REFERENCES sys_table_occurrences(id) ON DELETE CASCADE;
-- ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS related_occurrence_id VARCHAR(36),
--     ADD CONSTRAINT fk_value_list_related_occurrence_id FOREIGN KEY IF NOT EXISTS (related_occurrence_id)
--     REFERENCES sys_table_occurrences(id) ON DELETE CASCADE;
