-- File4Base System Catalog Migration 003: repeating fields
-- (PostgreSQL & MariaDB compatible)
--
-- A field holds up to max_repetitions values, as a FileMaker repeating
-- field does (1 to 32000; 1 is a field that does not repeat). Repetition 1
-- stays in the field's own column. Repetitions 2..N of a field <name> are
-- kept as a JSON array (element 0 = repetition 2) in a text column
-- "<name>$reps" of the user table, added when the field first gets more than
-- one repetition (see server/internal/repetition). The server applies this
-- statement itself on start-up (schema.EnsureSystemTables).

ALTER TABLE sys_columns ADD COLUMN IF NOT EXISTS max_repetitions INT NOT NULL DEFAULT 1;
