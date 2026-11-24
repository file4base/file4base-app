-- File4Base System Catalog Migration 005 (PostgreSQL & MariaDB)
--
-- External table occurrences (#107): an occurrence of a table of another
-- solution, reached through a file reference (FileMaker's External Data
-- Source). Every new column is nullable; a catalog without external
-- occurrences reads and writes exactly as before.
--
--   sys_table_occurrences.file_reference_id  the file reference the
--                                            occurrence goes through; NULL
--                                            for an occurrence of a table of
--                                            this solution.
--   sys_table_occurrences.external_table     the table's name in the other
--                                            solution, empty until the
--                                            reference is bound.
--   sys_file_references.paths                the FileMaker paths of the data
--                                            source, one per line.
--   sys_value_lists.source_column_name       the field of a list taken from a
--                                            field of another solution's
--                                            table, by name.
--   sys_columns.source_field_id              the FileMaker id of the field a
--                                            migrated column comes from; NULL
--                                            for a column created here.
--
-- A field of another file known only by its FileMaker id (FileMaker writes
-- an empty name when the other file was unreachable at export time) is
-- named "#<id>" wherever a name is stored: an identifier never contains
-- "#", and binding the file reference finds the field by its
-- source_field_id in the other solution.
--
-- An external occurrence has no base table, and a relationship side or a
-- predicate through it names its field instead of giving its id, so those
-- columns become optional.
--
-- The server applies this itself (schema.EnsureSystemTables, and the first
-- time an occurrence, a file reference or a value list is read or written on
-- an older database), reading the catalog first so an upgraded catalog is
-- only read.

ALTER TABLE sys_table_occurrences ADD COLUMN IF NOT EXISTS file_reference_id VARCHAR(36);
ALTER TABLE sys_table_occurrences ADD COLUMN IF NOT EXISTS external_table VARCHAR(64);
ALTER TABLE sys_file_references ADD COLUMN IF NOT EXISTS paths TEXT;
ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS source_column_name VARCHAR(64);
ALTER TABLE sys_columns ADD COLUMN IF NOT EXISTS source_field_id VARCHAR(32);

-- PostgreSQL.
ALTER TABLE sys_table_occurrences ALTER COLUMN base_table_id DROP NOT NULL;
ALTER TABLE sys_relationships ALTER COLUMN left_column_id DROP NOT NULL;
ALTER TABLE sys_relationships ALTER COLUMN right_column_id DROP NOT NULL;
ALTER TABLE sys_relationship_predicates ALTER COLUMN left_column_id DROP NOT NULL;
ALTER TABLE sys_relationship_predicates ALTER COLUMN right_column_id DROP NOT NULL;

-- MariaDB, in place of the five statements above:
--
-- ALTER TABLE sys_table_occurrences MODIFY base_table_id VARCHAR(36) NULL;
-- ALTER TABLE sys_relationships MODIFY left_column_id VARCHAR(36) NULL;
-- ALTER TABLE sys_relationships MODIFY right_column_id VARCHAR(36) NULL;
-- ALTER TABLE sys_relationship_predicates MODIFY left_column_id VARCHAR(36) NULL;
-- ALTER TABLE sys_relationship_predicates MODIFY right_column_id VARCHAR(36) NULL;
