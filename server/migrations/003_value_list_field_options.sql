-- File4Base System Catalog Migration 003 (PostgreSQL & MariaDB compatible)
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
-- time a value list is read or written on an older database). On MariaDB each
-- foreign key is added separately, named fk_value_list_<column>, with
-- FOREIGN KEY IF NOT EXISTS, because MariaDB adds an inline REFERENCES again
-- even when ADD COLUMN IF NOT EXISTS skips the column.

ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS second_column_id VARCHAR(36) REFERENCES sys_columns(id) ON DELETE SET NULL;
ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS show_second_only BOOLEAN;
ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS sort_by VARCHAR(16);
ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS source_occurrence_id VARCHAR(36) REFERENCES sys_table_occurrences(id) ON DELETE CASCADE;
ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS related_occurrence_id VARCHAR(36) REFERENCES sys_table_occurrences(id) ON DELETE CASCADE;
